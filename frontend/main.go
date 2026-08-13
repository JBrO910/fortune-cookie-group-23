package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var BACKEND_DNS = getEnv("BACKEND_DNS", "localhost")
var BACKEND_PORT = getEnv("BACKEND_PORT", "9000")
var templatePath = "./templates/fortunes.html"
var staticPath = "./static"
var httpListenAndServe = http.ListenAndServe

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
)

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

// withMetrics wraps a handler to record request count and latency, labelled
// by the registered route pattern rather than the raw URL path.
func withMetrics(pattern string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)
		next(rw, r)
		httpRequestsTotal.WithLabelValues(r.Method, pattern, strconv.Itoa(rw.statusCode)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, pattern).Observe(time.Since(start).Seconds())
	}
}

type fortune struct {
	ID      string `json:"id" redis:"id"`
	Message string `json:"message" redis:"message"`
}

type newFortune struct {
	Message string `json:"message"`
}

// use a custom client, because we don't do blocking operations wihout timeouts
var myClient = &http.Client{Timeout: 10 * time.Second}

func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "healthy")
}

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", HealthzHandler)
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/api/random", withMetrics("/api/random", func(w http.ResponseWriter, r *http.Request) {
		resp, err := myClient.Get(fmt.Sprintf("http://%s:%s/fortunes/random", BACKEND_DNS, BACKEND_PORT))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		f := new(fortune)
		if err := json.NewDecoder(resp.Body).Decode(f); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = fmt.Fprint(w, f.Message)
	}))

	mux.HandleFunc("/api/all", withMetrics("/api/all", func(w http.ResponseWriter, r *http.Request) {
		resp, err := myClient.Get(fmt.Sprintf("http://%s:%s/fortunes", BACKEND_DNS, BACKEND_PORT))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		fortunes := new([]fortune)
		if err := json.NewDecoder(resp.Body).Decode(fortunes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl, err := template.ParseFiles(templatePath)

		if err != nil {
			log.Println("Error parsing template:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, fortunes); err != nil {
			log.Println(err)
		}
	}))

	mux.HandleFunc("/api/add", withMetrics("/api/add", func(w http.ResponseWriter, r *http.Request) {

		if r.Method != "POST" {
			http.Error(w, "Use POST", http.StatusMethodNotAllowed)
			return
		}

		f := new(newFortune)
		if err := json.NewDecoder(r.Body).Decode(f); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var postUrl = fmt.Sprintf("http://%s:%s/fortunes", BACKEND_DNS, BACKEND_PORT)
		var jsonStr = []byte(fmt.Sprintf(`{"id": "%d", "message": "%s"}`, rand.Intn(10000), f.Message))

		_, err := myClient.Post(postUrl, "application/json", bytes.NewBuffer(jsonStr))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		_, _ = fmt.Fprint(w, "Cookie added!")
	}))

	mux.Handle("/", withMetrics("/", http.FileServer(http.Dir(staticPath)).ServeHTTP))
}

func main() {
	registerRoutes(http.DefaultServeMux)
	err := httpListenAndServe(":8080", nil)
	fmt.Printf("%v", err)
}
