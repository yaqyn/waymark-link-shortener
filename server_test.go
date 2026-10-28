package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func Test_requestLogger(t *testing.T) {
	logBuffer := &bytes.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "duration" {
				return slog.Duration("duration", time.Millisecond)
			}
			if a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			return a
		},
	}))

	requestLoggerMiddleware := requestLogger(logger)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	loggedHandler := requestID(requestLoggerMiddleware(dummyHandler))

	req := httptest.NewRequest("GET", "http://lin.ko/api/stats", nil)
	req.Header.Set("X-Request-ID", "test-request-id")
	rr := httptest.NewRecorder()
	loggedHandler.ServeHTTP(rr, req)

	const expectedLogString = `time=2023-10-01T12:34:57.000Z level=INFO msg="Served request" method=GET path=/api/stats client_ip=192.0.2.x request_id=test-request-id duration=1ms request_body_bytes=0 response_status=200 response_body_bytes=0` + "\n"
	const expectedStatusCode = http.StatusOK

	if got := logBuffer.String(); got != expectedLogString {
		t.Errorf("request log mismatch: got %q, want %q", got, expectedLogString)
	}
	if got := rr.Header().Get("X-Request-ID"); got != "test-request-id" {
		t.Errorf("response request ID mismatch: got %q, want %q", got, "test-request-id")
	}
	if rr.Code != expectedStatusCode {
		t.Errorf("response status mismatch: got %d, want %d", rr.Code, expectedStatusCode)
	}
}

func TestSecurityHeadersIsolateBrowserCapabilities(t *testing.T) {
	response := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Header().Get("Permissions-Policy") == "" || response.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatal("missing browser isolation headers")
	}
}
