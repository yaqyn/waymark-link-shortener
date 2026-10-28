package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/yaqyn/waymark-link-shortener/internal/apperr"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yaqyn/waymark-link-shortener/internal/store"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type server struct {
	httpServer *http.Server
	store      *store.Store
	cancel     context.CancelFunc
	logger     *slog.Logger
	auth       *authState
}

func newServer(store *store.Store, port int, cancel context.CancelFunc, logger *slog.Logger) *server {
	mux := http.NewServeMux()
	host := os.Getenv("WAYMARK_BIND_ADDR")
	if host == "" {
		host = "127.0.0.1"
	}

	srv := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	s := &server{
		httpServer: srv,
		store:      store,
		cancel:     cancel,
		logger:     logger,
		auth:       newAuth("frodo", demoHash, false),
	}

	mux.HandleFunc("GET /{$}", s.handlerIndex)
	mux.Handle("GET /assets/", staticFiles())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	if os.Getenv("ENV") == "production" {
		mux.Handle("GET /metrics", s.authMiddleware(promhttp.Handler()))
	} else {
		mux.Handle("GET /metrics", promhttp.Handler())
	}
	if os.Getenv("ENV") != "production" {
		mux.Handle("GET /debug/pprof/", s.authMiddleware(http.HandlerFunc(pprof.Index)))
		mux.Handle("GET /debug/pprof/profile", s.authMiddleware(http.HandlerFunc(pprof.Profile)))
	}
	mux.HandleFunc("POST /api/login", s.handlerLogin)
	mux.HandleFunc("POST /api/logout", s.handlerLogout)
	mux.Handle("GET /api/session", s.authMiddleware(http.HandlerFunc(s.handlerSession)))
	mux.Handle("PATCH /api/urls/{shortCode}", s.authMiddleware(http.HandlerFunc(s.handlerUpdateLink)))
	mux.Handle("POST /api/shorten", s.authMiddleware(http.HandlerFunc(s.handlerShortenLink)))
	mux.Handle("GET /api/stats", s.authMiddleware(http.HandlerFunc(s.handlerStats)))
	mux.Handle("GET /api/urls", s.authMiddleware(http.HandlerFunc(s.handlerListURLs)))
	mux.HandleFunc("GET /{shortCode}", s.handlerRedirect)
	if os.Getenv("ENV") != "production" {
		mux.Handle("POST /admin/shutdown", s.authMiddleware(http.HandlerFunc(s.handlerShutdown)))
	}
	srv.Handler = requestID(securityHeaders(otelhttp.NewHandler(requestLogger(logger)(metricsMiddleware(recoverPanics(sameOrigin(mux)))), "http.server", otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/metrics" && r.URL.Path != "/healthz" }))))

	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}

	s.logger.Info("Waymark started", "port", ln.Addr().(*net.TCPAddr).Port)
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *server) handlerShutdown(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.shutdown")
	defer span.End()
	r = r.WithContext(ctx)

	if os.Getenv("ENV") == "production" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.cancel()
}

type LogContext struct {
	Username string
	Error    error
}

const LogContextKey contextKey = "log_context"

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logCtx := &LogContext{}
			r = r.WithContext(context.WithValue(r.Context(), LogContextKey, logCtx))
			spyReader := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyReader
			spyWriter := &spyResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(spyWriter, r)
			if spyWriter.statusCode < 400 && (r.URL.Path == "/metrics" || r.URL.Path == "/healthz") {
				return
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", redactIP(r.RemoteAddr)),
				slog.String("request_id", w.Header().Get("X-Request-ID")),
				slog.Duration("duration", time.Since(start)),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", spyWriter.statusCode),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
			}
			if logCtx.Username != "" {
				attrs = append(attrs, slog.String("user", logCtx.Username))
			}
			if logCtx.Error != nil {
				attrs = append(attrs, slog.Any("error", logCtx.Error))
			}
			sc := trace.SpanContextFromContext(r.Context())
			if sc.IsValid() {
				attrs = append(attrs, slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
			}
			level := slog.LevelInfo
			if spyWriter.statusCode >= 500 {
				level = slog.LevelError
			} else if spyWriter.statusCode >= 400 {
				level = slog.LevelWarn
			}
			logger.LogAttrs(r.Context(), level, "Served request", attrs...)
		})
	}
}

type spyReadCloser struct {
	io.ReadCloser
	bytesRead int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += n
	return n, err
}

type spyResponseWriter struct {
	http.ResponseWriter
	bytesWritten int
	statusCode   int
	wroteHeader  bool
}

func (w *spyResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	return n, err
}

func (w *spyResponseWriter) WriteHeader(statusCode int) {
	if statusCode >= 100 && statusCode < 200 && statusCode != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *spyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func httpError(ctx context.Context, w http.ResponseWriter, statusCode int, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	if statusCode >= 500 {
		span.SetStatus(codes.Error, http.StatusText(statusCode))
	}
	if logCtx, ok := ctx.Value(LogContextKey).(*LogContext); ok && logCtx != nil {
		logCtx.Error = err
	}
	message := err.Error()
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError:
		message = http.StatusText(statusCode)
	}
	http.Error(w, message, statusCode)
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = ""
		}
		if id == "" {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func redactIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return address
	}
	ipv4 := ip.To4().String()
	return ipv4[:strings.LastIndexByte(ipv4, '.')+1] + "x"
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				httpError(r.Context(), w, 500, apperr.WithAttrs(errors.New("handler panic"), "stack_trace", string(debug.Stack()), "panic_type", fmt.Sprintf("%T", recovered)))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
