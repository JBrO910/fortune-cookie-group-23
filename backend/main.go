package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listFortuneRe   = regexp.MustCompile(`^/fortunes[/]*$`)
	getFortuneRe    = regexp.MustCompile(`^/fortunes[/](\d+)$`)
	randomFortuneRe = regexp.MustCompile(`^/fortunes[/]random$`)
	createFortuneRe = regexp.MustCompile(`^/fortunes[/]*$`)
)

// --- Prometheus metrics ---
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fortune_http_requests_total",
			Help: "Total number of HTTP requests, labelled by method, path and status code.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fortune_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	fortuneCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "fortune_store_count",
			Help: "Current number of fortunes in the in-memory store.",
		},
	)
)

type fortune struct {
	ID      string `json:"id" redis:"id"`
	Message string `json:"message" redis:"message"`
}

type datastore struct {
	m map[string]fortune
	*sync.RWMutex
}

var datastoreDefault = datastore{m: map[string]fortune{
	"1": {ID: "1", Message: "A new voyage will fill your life with untold memories."},
	"2": {ID: "2", Message: "The measure of time to your next goal is the measure of your discipline."},
	"3": {ID: "3", Message: "The only way to do well is to do better each day."},
	"4": {ID: "4", Message: "It ain't over till it's EOF."},
}, RWMutex: &sync.RWMutex{}}

type fortuneHandler struct {
	store *datastore
}

func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("healthy"))
}

// responseWriter wraps http.ResponseWriter to capture the status code for metrics.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (h *fortuneHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := newResponseWriter(w)
	rw.Header().Set("content-type", "application/json")

	path := r.URL.Path // capture before possible mutation (Random rewrites it)

	switch {
	case r.Method == http.MethodGet && listFortuneRe.MatchString(r.URL.Path):
		h.List(rw, r)
	case r.Method == http.MethodGet && getFortuneRe.MatchString(r.URL.Path):
		h.Get(rw, r)
	case r.Method == http.MethodGet && randomFortuneRe.MatchString(r.URL.Path):
		h.Random(rw, r)
	case r.Method == http.MethodPost && createFortuneRe.MatchString(r.URL.Path):
		h.Create(rw, r)
	default:
		notFound(rw, r)
	}

	duration := time.Since(start).Seconds()
	statusStr := strconv.Itoa(rw.statusCode)
	httpRequestsTotal.WithLabelValues(r.Method, path, statusStr).Inc()
	httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
}

func (h *fortuneHandler) List(w http.ResponseWriter, r *http.Request) {
	h.store.RLock()
	fortunes := make([]fortune, 0, len(h.store.m))
	for _, v := range h.store.m {
		fortunes = append(fortunes, v)
	}
	h.store.RUnlock()

	jsonBytes, err := json.Marshal(fortunes)
	if err != nil {
		internalServerError(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonBytes)
}

func (h *fortuneHandler) Random(w http.ResponseWriter, r *http.Request) {
	h.store.RLock()
	fortunes := make([]fortune, 0, len(h.store.m))
	for _, v := range h.store.m {
		fortunes = append(fortunes, v)
	}
	h.store.RUnlock()

	if len(fortunes) > 0 {
		u := fortunes[rand.Intn(len(fortunes))]
		r.URL.Path = "/fortunes/" + u.ID
	} else {
		r.URL.Path = "/fortunes/zero"
	}

	h.Get(w, r)
}

func (h *fortuneHandler) Get(w http.ResponseWriter, r *http.Request) {
	matches := getFortuneRe.FindStringSubmatch(r.URL.Path)
	if len(matches) < 2 {
		notFound(w, r)
		return
	}

	if usingRedis {
		conn := dbPool.Get()
		key := matches[1]
		val, err := conn.Do("hget", "fortunes", key)
		_ = conn.Close()
		if err != nil {
			fmt.Println("redis hget failed", err.Error())
		} else {
			if val != nil {
				msg := string(val.([]byte))
				h.store.Lock()
				h.store.m[key] = fortune{ID: key, Message: msg}
				h.store.Unlock()
			}
		}
	}

	h.store.RLock()
	u, ok := h.store.m[matches[1]]
	h.store.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("fortune not found"))
		return
	}
	jsonBytes, err := json.Marshal(u)
	if err != nil {
		internalServerError(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonBytes)
}

func (h *fortuneHandler) Create(w http.ResponseWriter, r *http.Request) {
	var u fortune
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		internalServerError(w, r)
		return
	}
	h.store.Lock()
	h.store.m[u.ID] = u
	count := len(h.store.m)
	h.store.Unlock()

	fortuneCount.Set(float64(count))

	if usingRedis {
		conn := dbPool.Get()
		_, err := conn.Do("hset", "fortunes", u.ID, u.Message)
		_ = conn.Close()
		if err != nil {
			fmt.Println("redis hset failed", err.Error())
		}
	}

	jsonBytes, err := json.Marshal(u)
	if err != nil {
		internalServerError(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonBytes)
}

func internalServerError(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("internal server error"))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("not found"))
}

func main() {
	// Seed the fortune count gauge with the initial store size.
	fortuneCount.Set(float64(len(datastoreDefault.m)))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", HealthzHandler)
	mux.Handle("/metrics", promhttp.Handler())
	fortuneH := &fortuneHandler{
		store: &datastoreDefault,
	}
	mux.Handle("/fortunes", fortuneH)
	mux.Handle("/fortunes/", fortuneH)

	err := http.ListenAndServe(":9000", mux)
	fmt.Printf("%v\n", err)
}
