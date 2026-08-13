package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestClient(fn roundTripFunc) *http.Client {
	return &http.Client{
		Transport: fn,
	}
}

func newMuxWithTestDeps(t *testing.T, client *http.Client) *http.ServeMux {
	t.Helper()
	prevClient := myClient
	prevFatal := logFatal
	prevTemplatePath := templatePath
	prevStaticPath := staticPath

	myClient = client
	logFatal = func(v ...interface{}) {}

	tmpDir := t.TempDir()
	templatePath = filepath.Join(tmpDir, "fortunes.html")
	staticPath = tmpDir

	t.Cleanup(func() {
		myClient = prevClient
		logFatal = prevFatal
		templatePath = prevTemplatePath
		staticPath = prevStaticPath
	})

	mux := http.NewServeMux()
	registerRoutes(mux)
	return mux
}

func TestHealthz(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(HealthzHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
	if rr.Body.String() != "healthy" {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), "healthy")
	}
}

func TestGetEnv(t *testing.T) {
	const key = "TEST_FRONTEND_ENV_KEY"
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

func TestAPIRandomSuccess(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		if req.URL.Path != "/fortunes/random" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"1","message":"fortune!"}`)),
			Header:     make(http.Header),
		}, nil
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "fortune!" {
		t.Fatalf("expected body %q, got %q", "fortune!", rr.Body.String())
	}
}

func TestAPIRandomBackendError(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("backend down")
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "backend down") {
		t.Fatalf("expected error response body, got %q", rr.Body.String())
	}
}

func TestAPIAllSuccess(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[{"id":"1","message":"first"},{"id":"2","message":"second"}]`)),
			Header:     make(http.Header),
		}, nil
	})

	mux := newMuxWithTestDeps(t, client)
	if err := os.WriteFile(templatePath, []byte(`{{range .}}{{.Message}};{{end}}`), 0644); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/all", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "first;second;" {
		t.Fatalf("unexpected body %q", rr.Body.String())
	}
}

func TestAPIAllTemplateError(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
			Header:     make(http.Header),
		}, nil
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodGet, "/api/all", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	errText := strings.ToLower(rr.Body.String())
	if !strings.Contains(errText, "cannot find") && !strings.Contains(errText, "no such file or directory") {
		t.Fatalf("expected template parse error in body, got %q", rr.Body.String())
	}
}

func TestAPIAllBackendError(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("backend unavailable")
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodGet, "/api/all", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "backend unavailable") {
		t.Fatalf("expected backend error in body, got %q", rr.Body.String())
	}
}

func TestAPIAddMethodNotAllowed(t *testing.T) {
	mux := newMuxWithTestDeps(t, newTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(``)), Header: make(http.Header)}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/add", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Use POST") {
		t.Fatalf("expected method error body, got %q", rr.Body.String())
	}
}

func TestAPIAddSuccess(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/fortunes" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if !strings.Contains(string(bodyBytes), `"message": "hello"`) {
			t.Fatalf("expected outgoing message payload, got %q", string(bodyBytes))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(`{"message":"hello"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "Cookie added!" {
		t.Fatalf("expected success body, got %q", rr.Body.String())
	}
}

func TestAPIAddBackendError(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("post failed")
	})

	mux := newMuxWithTestDeps(t, client)
	req := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(`{"message":"hello"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "post failed") {
		t.Fatalf("expected backend error in body, got %q", rr.Body.String())
	}
}

func TestStaticFileHandler(t *testing.T) {
	mux := newMuxWithTestDeps(t, newTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(``)), Header: make(http.Header)}, nil
	}))

	filePath := filepath.Join(staticPath, "index.html")
	if err := os.WriteFile(filePath, []byte("frontend"), 0644); err != nil {
		t.Fatalf("failed to create static file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "frontend" {
		t.Fatalf("unexpected static file response %q", rr.Body.String())
	}
}

func TestMainReturnsWhenPortInUse(t *testing.T) {
	prevListen := httpListenAndServe
	httpListenAndServe = func(addr string, handler http.Handler) error {
		if addr != ":8080" {
			t.Fatalf("expected addr :8080, got %s", addr)
		}
		return errors.New("listen failed")
	}
	defer func() { httpListenAndServe = prevListen }()

	main()
}
