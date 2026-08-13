package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockRedisConn struct {
	doFunc func(commandName string, args ...interface{}) (reply interface{}, err error)
}

func (m *mockRedisConn) Close() error {
	return nil
}

func (m *mockRedisConn) Err() error {
	return nil
}

func (m *mockRedisConn) Do(commandName string, args ...interface{}) (reply interface{}, err error) {
	if m.doFunc != nil {
		return m.doFunc(commandName, args...)
	}
	return nil, nil
}

func (m *mockRedisConn) Send(commandName string, args ...interface{}) error {
	return nil
}

func (m *mockRedisConn) Flush() error {
	return nil
}

func (m *mockRedisConn) Receive() (reply interface{}, err error) {
	return nil, nil
}

func newTestHandler(seed map[string]fortune) *fortuneHandler {
	m := make(map[string]fortune, len(seed))
	for k, v := range seed {
		m[k] = v
	}

	return &fortuneHandler{
		store: &datastore{
			m:       m,
			RWMutex: &sync.RWMutex{},
		},
	}
}

func forceNoRedis(t *testing.T) {
	t.Helper()
	prev := usingRedis
	usingRedis = false
	t.Cleanup(func() {
		usingRedis = prev
	})
}

func forceRedisConn(t *testing.T, conn mockRedisConn) {
	t.Helper()
	prevUsingRedis := usingRedis
	prevConn := dbLink
	usingRedis = true
	dbLink = &conn
	t.Cleanup(func() {
		usingRedis = prevUsingRedis
		dbLink = prevConn
	})
}

func TestGetEnv(t *testing.T) {
	const key = "TEST_BACKEND_ENV_KEY"
	original, hadOriginal := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	defer func() {
		if hadOriginal {
			_ = os.Setenv(key, original)
		} else {
			_ = os.Unsetenv(key)
		}
	}()

	if got := getEnv(key, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}

	_ = os.Setenv(key, "configured")
	if got := getEnv(key, "fallback"); got != "configured" {
		t.Fatalf("expected configured value, got %q", got)
	}
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(HealthzHandler)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "healthy" {
		t.Fatalf("expected body %q, got %q", "healthy", rr.Body.String())
	}
}

func TestServeHTTPList(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{
		"1": {ID: "1", Message: "first"},
		"2": {ID: "2", Message: "second"},
	})

	req := httptest.NewRequest(http.MethodGet, "/fortunes", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got []fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 fortunes, got %d", len(got))
	}
}

func TestServeHTTPGetByID(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{
		"42": {ID: "42", Message: "answer"},
	})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/42", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if got.ID != "42" || got.Message != "answer" {
		t.Fatalf("unexpected fortune: %v", got)
	}
}

func TestServeHTTPGetByIDRedisValue(t *testing.T) {
	forceRedisConn(t, mockRedisConn{
		doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
			return []byte("from redis"), nil
		},
	})

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/8", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if got.ID != "8" || got.Message != "from redis" {
		t.Fatalf("unexpected fortune: %v", got)
	}
}

func TestServeHTTPGetByIDRedisErrorFallsBackToStore(t *testing.T) {
	forceRedisConn(t, mockRedisConn{
		doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
			return nil, errors.New("redis unavailable")
		},
	})

	h := newTestHandler(map[string]fortune{
		"9": {ID: "9", Message: "local copy"},
	})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/9", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if got.ID != "9" || got.Message != "local copy" {
		t.Fatalf("unexpected fortune: %v", got)
	}
}

func TestServeHTTPGetNotFound(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/999", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "fortune not found" {
		t.Fatalf("expected body %q, got %q", "fortune not found", rr.Body.String())
	}
}

func TestServeHTTPGetNotFoundRedisNilValue(t *testing.T) {
	forceRedisConn(t, mockRedisConn{
		doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
			return nil, nil
		},
	})

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/99", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "fortune not found" {
		t.Fatalf("expected body %q, got %q", "fortune not found", rr.Body.String())
	}
}

func TestServeHTTPCreate(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/fortunes",
		strings.NewReader(`{"id":"7","message":"lucky"}`),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if got.ID != "7" || got.Message != "lucky" {
		t.Fatalf("unexpected created fortune: %v", got)
	}

	h.store.RLock()
	created, ok := h.store.m["7"]
	h.store.RUnlock()
	if !ok || created.Message != "lucky" {
		t.Fatalf("fortune was not persisted in store: %v", created)
	}
}

func TestServeHTTPCreateWithRedis(t *testing.T) {
	redisCalled := false
	forceRedisConn(t, mockRedisConn{
		doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
			if commandName == "hset" {
				redisCalled = true
			}
			return nil, nil
		},
	})

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/fortunes",
		strings.NewReader(`{"id":"10","message":"saved in redis"}`),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !redisCalled {
		t.Fatal("expected redis hset to be called")
	}
}

func TestServeHTTPCreateWithRedisError(t *testing.T) {
	forceRedisConn(t, mockRedisConn{
		doFunc: func(commandName string, args ...interface{}) (interface{}, error) {
			return nil, errors.New("redis write failed")
		},
	})

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/fortunes",
		strings.NewReader(`{"id":"11","message":"still saved locally"}`),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func TestServeHTTPRandomEmptyStore(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/random", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "not found" {
		t.Fatalf("expected body %q, got %q", "not found", rr.Body.String())
	}
}

func TestServeHTTPRandomSingleFortune(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{
		"5": {ID: "5", Message: "single"},
	})

	req := httptest.NewRequest(http.MethodGet, "/fortunes/random", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var got fortune
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed decoding response: %v", err)
	}
	if got.ID != "5" || got.Message != "single" {
		t.Fatalf("unexpected fortune: %v", got)
	}
}

func TestServeHTTPCreateInvalidJSON(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/fortunes",
		strings.NewReader("{"),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rr.Code)
	}
	if rr.Body.String() != "internal server error" {
		t.Fatalf("expected body %q, got %q", "internal server error", rr.Body.String())
	}
}

func TestGetInvalidPath(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})
	req := httptest.NewRequest(http.MethodGet, "/fortunes/abc", nil)
	rr := httptest.NewRecorder()

	h.Get(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "not found" {
		t.Fatalf("expected body %q, got %q", "not found", rr.Body.String())
	}
}

func TestErrorHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rrNotFound := httptest.NewRecorder()
	notFound(rrNotFound, req)
	if rrNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rrNotFound.Code)
	}
	if rrNotFound.Body.String() != "not found" {
		t.Fatalf("expected body %q, got %q", "not found", rrNotFound.Body.String())
	}

	rrInternal := httptest.NewRecorder()
	internalServerError(rrInternal, req)
	if rrInternal.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rrInternal.Code)
	}
	if rrInternal.Body.String() != "internal server error" {
		t.Fatalf("expected body %q, got %q", "internal server error", rrInternal.Body.String())
	}
}

func TestServeHTTPUnknownRoute(t *testing.T) {
	forceNoRedis(t)

	h := newTestHandler(map[string]fortune{})

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Body.String() != "not found" {
		t.Fatalf("expected body %q, got %q", "not found", rr.Body.String())
	}
}

func TestMainReturnsWhenPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", ":9000")
	if err == nil {
		defer ln.Close()
	}

	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected main to return quickly when port is already in use")
	}
}
