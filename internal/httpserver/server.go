// Package httpserver wires HelloCalc's HTTP routes, middleware and server
// lifecycle.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith/internal/calculator"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith/web"
)

// maxBodyBytes bounds the size of API request bodies.
const maxBodyBytes = 4 << 10

// HTTP server timeouts.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 16 << 10
)

// Server serves the HelloCalc UI, API and operational endpoints.
type Server struct {
	logger  *slog.Logger
	info    buildinfo.Info
	ready   atomic.Bool
	handler http.Handler
}

// New returns a Server. It reports not-ready until Serve starts listening
// or SetReady(true) is called.
func New(logger *slog.Logger, info buildinfo.Info) *Server {
	s := &Server{logger: logger, info: info}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/calculate", s.handleCalculate)
	mux.HandleFunc("GET /health", s.handleHealthz) // deploy.yaml healthcheck path
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /version", s.handleVersion)
	registerStatic(mux, web.Assets)

	s.handler = withRequestID(withLogging(logger, withRecover(logger, withSecurityHeaders(mux))))
	return s
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler { return s.handler }

// SetReady controls the readiness endpoint.
func (s *Server) SetReady(ready bool) { s.ready.Store(ready) }

// Serve serves HTTP on ln until ctx is cancelled, then shuts down gracefully:
// readiness is withdrawn, the listener is closed, and in-flight requests get up
// to shutdownTimeout to complete.
func (s *Server) Serve(ctx context.Context, ln net.Listener, shutdownTimeout time.Duration) error {
	return serve(ctx, ln, s.handler, shutdownTimeout, s.logger, s.SetReady)
}

func serve(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger, setReady func(bool)) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	setReady(true)
	logger.Info("server listening", slog.String("addr", ln.Addr().String()))

	select {
	case err := <-errCh:
		setReady(false)
		return err
	case <-ctx.Done():
	}

	setReady(false)
	logger.Info("shutting down", slog.String("timeout", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("server stopped")
	return nil
}

// registerStatic serves every embedded asset at its own path, with
// index.html at "/". Only exact paths are registered so unknown paths
// return 404 and API paths still report 405 for wrong methods.
func registerStatic(mux *http.ServeMux, assets fs.FS) {
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		panic("httpserver: reading embedded assets: " + err.Error())
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		pattern := "GET /" + name
		if name == "index.html" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, assets, name)
		})
	}
}

type calculateRequest struct {
	Left     *float64 `json:"left"`
	Operator *string  `json:"operator"`
	Right    *float64 `json:"right"`
}

type calculateResponse struct {
	Result float64 `json:"result"`
}

func (s *Server) handleCalculate(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req calculateRequest
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return
	}
	if req.Left == nil || req.Operator == nil || req.Right == nil {
		writeError(w, http.StatusBadRequest, "fields left, operator and right are required")
		return
	}

	result, err := calculator.Calculate(*req.Left, *req.Operator, *req.Right)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, calculateResponse{Result: result})
	case errors.Is(err, calculator.ErrUnsupportedOperator), errors.Is(err, calculator.ErrInvalidOperand):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		// Well-formed request whose arithmetic has no valid result
		// (division by zero, overflow, non-real result).
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.info)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal server error"}`)
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
