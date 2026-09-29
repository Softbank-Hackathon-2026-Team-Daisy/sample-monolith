package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith/internal/buildinfo"
)

func newTestServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := NewLogger(&logs, slog.LevelDebug)
	s := New(logger, buildinfo.Get())
	s.SetReady(true)
	return s, &logs
}

func do(t *testing.T, h http.Handler, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestCalculateSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct {
		body string
		want float64
	}{
		{`{"left": 1, "operator": "+", "right": 2}`, 3},
		{`{"left": 10, "operator": "/", "right": 4}`, 2.5},
		{`{"left": -7, "operator": "*", "right": 3}`, -21},
		{`{"left": 12.5, "operator": "*", "right": 4}`, 50},
		{`{"left": 3, "operator": "-", "right": 5.5}`, -2.5},
		{`{"left": 7, "operator": "%", "right": 4}`, 3},
		{`{"left": 2, "operator": "^", "right": 8}`, 256},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			rec := do(t, s.Handler(), http.MethodPost, "/api/calculate", "application/json", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if got := decode(t, rec)["result"]; got != tt.want {
				t.Fatalf("result = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateContentTypeWithCharset(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), http.MethodPost, "/api/calculate", "application/json; charset=utf-8", `{"left":1,"operator":"+","right":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestCalculateErrors(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		errMsg      string
	}{
		{"division by zero", "application/json", `{"left":1,"operator":"/","right":0}`, http.StatusUnprocessableEntity, "division by zero"},
		{"modulo by zero", "application/json", `{"left":1,"operator":"%","right":0}`, http.StatusUnprocessableEntity, "division by zero"},
		{"overflow", "application/json", `{"left":1e308,"operator":"*","right":10}`, http.StatusUnprocessableEntity, "result out of range"},
		{"unsupported operator", "application/json", `{"left":1,"operator":"&","right":2}`, http.StatusBadRequest, "unsupported operator"},
		{"malformed JSON", "application/json", `{"left":1,`, http.StatusBadRequest, "invalid JSON request body"},
		{"empty body", "application/json", ``, http.StatusBadRequest, "invalid JSON request body"},
		{"string operand", "application/json", `{"left":"1","operator":"+","right":2}`, http.StatusBadRequest, "invalid JSON request body"},
		{"number too large", "application/json", `{"left":1e400,"operator":"+","right":2}`, http.StatusBadRequest, "invalid JSON request body"},
		{"unknown field", "application/json", `{"left":1,"operator":"+","right":2,"extra":true}`, http.StatusBadRequest, "invalid JSON request body"},
		{"array body", "application/json", `[1,"+",2]`, http.StatusBadRequest, "invalid JSON request body"},
		{"missing field", "application/json", `{"left":1,"operator":"+"}`, http.StatusBadRequest, "fields left, operator and right are required"},
		{"null field", "application/json", `{"left":null,"operator":"+","right":2}`, http.StatusBadRequest, "fields left, operator and right are required"},
		{"trailing data", "application/json", `{"left":1,"operator":"+","right":2} {}`, http.StatusBadRequest, "request body must contain a single JSON object"},
		{"missing content type", "", `{"left":1,"operator":"+","right":2}`, http.StatusUnsupportedMediaType, "content type must be application/json"},
		{"wrong content type", "text/plain", `{"left":1,"operator":"+","right":2}`, http.StatusUnsupportedMediaType, "content type must be application/json"},
		{"body too large", "application/json", `{"left":1,"operator":"+","right":2,"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge, "request body too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, s.Handler(), http.MethodPost, "/api/calculate", tt.contentType, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body)
			}
			if got := decode(t, rec)["error"]; got != tt.errMsg {
				t.Fatalf("error = %q, want %q", got, tt.errMsg)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct{ method, path, allow string }{
		{http.MethodGet, "/api/calculate", "POST"},
		{http.MethodPut, "/api/calculate", "POST"},
		{http.MethodPost, "/healthz", "GET, HEAD"},
		{http.MethodDelete, "/version", "GET, HEAD"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(t, s.Handler(), tt.method, tt.path, "", "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != tt.allow {
				t.Fatalf("Allow = %q, want %q", got, tt.allow)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetReady(false) // liveness is independent of readiness
	for _, path := range []string{"/health", "/healthz"} {
		rec := do(t, s.Handler(), http.MethodGet, path, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if got := decode(t, rec)["status"]; got != "ok" {
			t.Fatalf("%s status field = %v", path, got)
		}
	}
}

func TestReadyz(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), http.MethodGet, "/readyz", "", "")
	if rec.Code != http.StatusOK || decode(t, rec)["status"] != "ready" {
		t.Fatalf("ready: status = %d, body = %s", rec.Code, rec.Body)
	}

	s.SetReady(false)
	rec = do(t, s.Handler(), http.MethodGet, "/readyz", "", "")
	if rec.Code != http.StatusServiceUnavailable || decode(t, rec)["status"] != "not ready" {
		t.Fatalf("not ready: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestVersion(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), http.MethodGet, "/version", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	m := decode(t, rec)
	want := buildinfo.Get()
	checks := map[string]string{
		"name":      "HelloCalc",
		"version":   want.Version,
		"commit":    want.Commit,
		"buildTime": want.BuildTime,
		"goVersion": want.GoVersion,
		"os":        want.OS,
		"arch":      want.Arch,
	}
	for k, v := range checks {
		if m[k] != v {
			t.Errorf("%s = %v, want %q", k, m[k], v)
		}
	}
}

func TestStaticAssets(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct{ path, contentType, contains string }{
		{"/", "text/html; charset=utf-8", "<title>HelloCalc</title>"},
		{"/app.js", "text/javascript; charset=utf-8", "/api/calculate"},
		{"/styles.css", "text/css; charset=utf-8", ".keys"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := do(t, s.Handler(), http.MethodGet, tt.path, "", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if !strings.Contains(rec.Body.String(), tt.contains) {
				t.Fatalf("body does not contain %q", tt.contains)
			}
		})
	}
}

func TestUnknownPath(t *testing.T) {
	s, _ := newTestServer(t)
	for _, p := range []string{"/nope", "/web.go", "/api/other"} {
		if rec := do(t, s.Handler(), http.MethodGet, p, "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", p, rec.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.Handler(), http.MethodGet, "/", "", "")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestRequestID(t *testing.T) {
	s, logs := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Fatalf("propagated X-Request-ID = %q, want abc-123", got)
	}

	rec = do(t, s.Handler(), http.MethodGet, "/healthz", "", "")
	generated := rec.Header().Get("X-Request-ID")
	if len(generated) != 32 {
		t.Fatalf("generated X-Request-ID = %q, want 32 hex chars", generated)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("a", maxRequestIDLen+1))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); len(got) != 32 {
		t.Fatalf("oversized X-Request-ID should be replaced, got %q", got)
	}

	if !strings.Contains(logs.String(), `"request_id":"abc-123"`) {
		t.Fatalf("request ID missing from logs: %s", logs)
	}
}

func TestRequestLog(t *testing.T) {
	s, logs := newTestServer(t)
	do(t, s.Handler(), http.MethodPost, "/api/calculate?secret=1", "application/json", `{"left":7777777.5,"operator":"+","right":1}`)

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("log is not a single JSON record: %q", logs)
	}
	for _, k := range []string{"timestamp", "level", "msg", "method", "path", "status", "duration_ms", "request_id"} {
		if _, ok := entry[k]; !ok {
			t.Errorf("log entry missing %q: %v", k, entry)
		}
	}
	if entry["path"] != "/api/calculate" || entry["status"] != float64(200) || entry["method"] != "POST" {
		t.Errorf("unexpected log entry: %v", entry)
	}
	if strings.Contains(logs.String(), "7777777") || strings.Contains(logs.String(), "secret") {
		t.Errorf("log leaks request input: %s", logs)
	}
}

func TestRecoverHidesPanicDetails(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := withRecover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("sensitive detail")
	}))
	rec := do(t, h, http.MethodGet, "/", "", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sensitive") {
		t.Fatalf("response leaks panic detail: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "sensitive detail") {
		t.Fatalf("panic not logged: %s", logs.String())
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	})

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan bool, 4)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(ctx, ln, h, 5*time.Second, logger, func(r bool) { ready <- r }) }()
	if !<-ready {
		t.Fatal("server did not report ready")
	}

	url := "http://" + ln.Addr().String() + "/"
	type result struct {
		body string
		err  error
	}
	inFlight := make(chan result, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			inFlight <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		inFlight <- result{string(b), err}
	}()

	<-started
	cancel()
	if <-ready {
		t.Fatal("server did not withdraw readiness on shutdown")
	}

	r := <-inFlight
	if r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request not completed: body=%q err=%v", r.body, r.err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("serve returned %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Fatal("server still accepting connections after shutdown")
	}
}

func TestServeShutdownTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(ctx, ln, h, 100*time.Millisecond, logger, func(bool) {}) }()

	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	cancel()

	select {
	case err := <-serveErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("serve returned %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not respect its timeout")
	}
}
