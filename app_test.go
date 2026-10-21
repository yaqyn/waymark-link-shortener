package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yaqyn/waymark-link-shortener/internal/store"
)

func testServer(t *testing.T) *server {
	t.Helper()
	t.Setenv("ENV", "development")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	return newServer(st, 8899, func() {}, logger)
}
func request(s *server, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		r.SetBasicAuth("frodo", "ofTheNineFingers")
	}
	rr := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr, r)
	return rr
}
func createTestLink(t *testing.T, s *server, code string) store.Link {
	t.Helper()
	rr := request(s, "POST", "/api/shorten", `{"url":"https://example.com/long-path","code":"`+code+`","title":"Example"}`, true)
	if rr.Code != 201 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var link store.Link
	if err := json.Unmarshal(rr.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	return link
}
func TestLinkLifecycle(t *testing.T) {
	s := testServer(t)
	if rr := request(s, "POST", "/api/shorten", `{"url":"https://example.com"}`, false); rr.Code != 401 {
		t.Fatalf("anonymous create: %d", rr.Code)
	}
	link := createTestLink(t, s, "example-link")
	if rr := request(s, "GET", "/example-link", "", false); rr.Code != 302 || rr.Header().Get("Location") != link.LongURL {
		t.Fatalf("redirect: %d %v", rr.Code, rr.Header())
	}
	persisted, err := s.store.Get(context.Background(), link.ShortCode)
	if err != nil || persisted.Clicks != 1 {
		t.Fatalf("click persistence: %+v %v", persisted, err)
	}
	rr := request(s, "PATCH", "/api/urls/example-link", `{"url":"https://example.org","title":"Changed","disabled":true,"revision":1}`, true)
	if rr.Code != 200 {
		t.Fatalf("update: %d %s", rr.Code, rr.Body)
	}
	if rr := request(s, "GET", "/example-link", "", false); rr.Code != 410 {
		t.Fatalf("disabled redirect: %d", rr.Code)
	}
	if rr := request(s, "PATCH", "/api/urls/example-link", `{"url":"https://example.net","revision":1}`, true); rr.Code != 409 {
		t.Fatalf("stale update: %d", rr.Code)
	}
	if rr := request(s, "PATCH", "/api/urls/example-link", `{"url":"https://example.org","revision":2}`, true); rr.Code != 200 {
		t.Fatalf("enable: %d", rr.Code)
	}
	if rr := request(s, "GET", "/example-link", "", false); rr.Code != 302 {
		t.Fatalf("enabled redirect: %d", rr.Code)
	}
	if rr := request(s, "GET", "/api/stats", "", true); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"redirects":2`) {
		t.Fatalf("stats: %d %s", rr.Code, rr.Body)
	}
}
func TestSessionAndCSRF(t *testing.T) {
	s := testServer(t)
	rr := request(s, "POST", "/api/login", `{"username":"frodo","password":"ofTheNineFingers"}`, false)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", cookies)
	}
	req := httptest.NewRequest("GET", "/api/session", nil)
	req.AddCookie(cookies[0])
	out := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatalf("session: %d", out.Code)
	}
	req = httptest.NewRequest("POST", "/api/logout", nil)
	req.AddCookie(cookies[0])
	req.Header.Set("Origin", "https://evil.example")
	out = httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatalf("CSRF: %d", out.Code)
	}
	req.Header.Del("Origin")
	out = httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(out, req)
	if out.Code != 204 {
		t.Fatalf("logout: %d", out.Code)
	}
	req = httptest.NewRequest("GET", "/api/session", nil)
	req.AddCookie(cookies[0])
	out = httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(out, req)
	if out.Code != 401 {
		t.Fatalf("revoked session: %d", out.Code)
	}
}
func TestDestinationValidationAndNoFetch(t *testing.T) {
	s := testServer(t)
	for _, url := range []string{"javascript:alert(1)", "ftp://example.com", "https://user:secret@example.com", "/relative", "https://", "https://example.com:0", "https://example.com:65536", "https://example.com:"} {
		body, _ := json.Marshal(map[string]string{"url": url})
		rr := request(s, "POST", "/api/shorten", string(body), true)
		if rr.Code != 400 {
			t.Errorf("accepted %q: %d", url, rr.Code)
		}
	}
	// Nothing is listening on this port: creation and redirect must not fetch it.
	rr := request(s, "POST", "/api/shorten", `{"url":"http://127.0.0.1:1","code":"no-fetch"}`, true)
	if rr.Code != 201 {
		t.Fatalf("unexpected destination fetch: %d %s", rr.Code, rr.Body)
	}
	if rr := request(s, "GET", "/no-fetch", "", false); rr.Code != 302 {
		t.Fatalf("redirect fetched destination: %d", rr.Code)
	}
	if rr := request(s, "POST", "/api/shorten", `{"url":"https://example.com","code":"metrics"}`, true); rr.Code != 400 {
		t.Fatalf("reserved alias: %d", rr.Code)
	}
	if rr := request(s, "POST", "/api/shorten", `{"url":"https://example.com","code":"no-fetch"}`, true); rr.Code != 409 {
		t.Fatalf("duplicate alias: %d", rr.Code)
	}
}

func TestDestinationNormalizesDomainCase(t *testing.T) {
	got, err := normalizeDestination("HTTPS://Example.COM:8443/SomePath")
	if err != nil || got != "https://example.com:8443/SomePath" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestListPageSize(t *testing.T) {
	s := testServer(t)
	createTestLink(t, s, "first-link")
	createTestLink(t, s, "second-link")
	for _, size := range []string{"0", "101", "invalid", "-1"} {
		rr := httptest.NewRecorder()
		s.handlerListURLs(rr, httptest.NewRequest("GET", "/api/urls?page_size="+size, nil))
		if rr.Code != 400 {
			t.Fatalf("page_size %s: %d", size, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	s.handlerListURLs(rr, httptest.NewRequest("GET", "/api/urls?page_size=1&page=2", nil))
	var body struct {
		Links    []store.Link `json:"links"`
		Page     int          `json:"page"`
		Pages    int          `json:"pages"`
		Total    int          `json:"total"`
		PageSize int          `json:"page_size"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || len(body.Links) != 1 || body.Page != 2 || body.Pages != 2 || body.Total != 2 || body.PageSize != 1 {
		t.Fatalf("response: %d %s", rr.Code, rr.Body)
	}
}
func TestListRejectsUnknownAndDuplicateParameters(t *testing.T) {
	s := testServer(t)
	for _, query := range []string{"q=one&q=two", "page=1&page=2", "unknown=value"} {
		rr := httptest.NewRecorder()
		s.handlerListURLs(rr, httptest.NewRequest("GET", "/api/urls?"+query, nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", query, rr.Code, rr.Body)
		}
	}
}
func TestDestinationRemovesDefaultPorts(t *testing.T) {
	for raw, want := range map[string]string{"http://Example.com:80/a": "http://example.com/a", "https://example.com:443/a": "https://example.com/a", "http://[::1]:80/a": "http://[::1]/a"} {
		got, err := normalizeDestination(raw)
		if err != nil || got != want {
			t.Fatalf("%s: got %q, %v", raw, got, err)
		}
	}
}
func TestDestinationCanonicalizesEmptyRootPath(t *testing.T) {
	got, err := normalizeDestination("https://example.com?source=course")
	if err != nil || got != "https://example.com/?source=course" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestListDisabledFilter(t *testing.T) {
	s := testServer(t)
	createTestLink(t, s, "enabled-link")
	createTestLink(t, s, "disabled-link")
	response := request(s, "PATCH", "/api/urls/disabled-link", `{"url":"https://example.com/long-path","disabled":true,"revision":1}`, true)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	for _, tc := range []struct{ query, code string }{
		{"true", "disabled-link"}, {"false", "enabled-link"},
	} {
		rr := httptest.NewRecorder()
		s.handlerListURLs(rr, httptest.NewRequest("GET", "/api/urls?disabled="+tc.query, nil))
		var result struct {
			Links []store.Link `json:"links"`
			Total int          `json:"total"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rr.Code != 200 || result.Total != 1 || len(result.Links) != 1 || result.Links[0].ShortCode != tc.code {
			t.Fatalf("filter %s: %d %s", tc.query, rr.Code, rr.Body)
		}
	}
	rr := httptest.NewRecorder()
	s.handlerListURLs(rr, httptest.NewRequest("GET", "/api/urls?disabled=unknown", nil))
	if rr.Code != 400 {
		t.Fatalf("invalid filter: %d", rr.Code)
	}
}
func TestExpiryAndPagination(t *testing.T) {
	s := testServer(t)
	expired := time.Now().Add(-time.Hour)
	_, err := s.store.CreateLink(context.Background(), store.Link{ShortCode: "expired", LongURL: "https://example.com", ExpiresAt: &expired})
	if err != nil {
		t.Fatal(err)
	}
	if rr := request(s, "GET", "/expired", "", false); rr.Code != 410 {
		t.Fatalf("expired link: %d", rr.Code)
	}
	for range 22 {
		if _, err := s.store.Create(context.Background(), "https://example.org"); err != nil {
			t.Fatal(err)
		}
	}
	rr := request(s, "GET", "/api/urls?page=2", "", true)
	var data struct {
		Links []store.Link
		Total int
		Page  int
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Links) != 3 || data.Total != 23 || data.Page != 2 {
		t.Fatalf("pagination: %+v", data)
	}
	rr = request(s, "GET", "/api/urls?q=missing&page=999", "", true)
	if !strings.Contains(rr.Body.String(), `"links":[]`) {
		t.Fatalf("empty list is not array: %s", rr.Body)
	}
}
func TestMetricsBoundedLabels(t *testing.T) {
	s := testServer(t)
	request(s, "GET", "/aaaa", "", false)
	request(s, "GET", "/bbbb", "", false)
	rr := request(s, "GET", "/metrics", "", false)
	if strings.Contains(rr.Body.String(), `path="/aaaa"`) || strings.Contains(rr.Body.String(), `path="/bbbb"`) {
		t.Fatal("unbounded shortcode labels")
	}
	if !strings.Contains(rr.Body.String(), `path="/{shortCode}"`) {
		t.Fatal("route label missing")
	}
	if !strings.Contains(rr.Body.String(), "http_request_duration_seconds") {
		t.Fatal("duration metric missing")
	}
}
func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: replaceAttr}))
	logger.Info("test", "Password", "secret-value", "user", "private-user", "url", "https://user:hidden@example.com/path")
	if strings.Contains(buf.String(), "secret-value") || strings.Contains(buf.String(), "private-user") || strings.Contains(buf.String(), "hidden") {
		t.Fatal("secret in logs")
	}
}
func TestPortConflictReturns(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("WAYMARK_TRACING", "")
	t.Setenv("WAYMARK_PASSWORD_HASH", "")
	t.Setenv("WAYMARK_PASSWORD_HASH_FILE", "")
	t.Setenv("ENV", "development")
	occupied := httptest.NewServer(http.NotFoundHandler())
	defer occupied.Close()
	address := occupied.Listener.Addr().(*net.TCPAddr)
	done := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- run(ctx, cancel, address.Port, t.TempDir()) }()
	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup hung on occupied port")
	}
}

func TestProductionBoundary(t *testing.T) {
	t.Setenv("ENV", "production")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(st, 8899, func() {}, logger)
	s.auth.secure = true
	if rr := request(s, "GET", "/metrics", "", false); rr.Code != 401 {
		t.Fatalf("production metrics: %d", rr.Code)
	}
	if rr := request(s, "GET", "/debug/pprof/", "", true); rr.Code != 404 {
		t.Fatalf("production pprof: %d", rr.Code)
	}
	if rr := request(s, "POST", "/admin/shutdown", "", true); rr.Code != 404 {
		t.Fatalf("production shutdown: %d", rr.Code)
	}
	rr := request(s, "POST", "/api/login", `{"username":"frodo","password":"ofTheNineFingers"}`, false)
	if rr.Code != 200 || !rr.Result().Cookies()[0].Secure {
		t.Fatal("production session not secure")
	}
	t.Setenv("WAYMARK_PASSWORD_HASH", "")
	t.Setenv("WAYMARK_PASSWORD_HASH_FILE", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("WAYMARK_TRACING", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if code := run(ctx, cancel, 0, t.TempDir()); code != 1 {
		t.Fatal("production allowed demo fallback")
	}
}
func TestLoginLimitAndOversizedPayload(t *testing.T) {
	s := testServer(t)
	// Password hashing may take longer than the window on a loaded race-test runner.
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	s.auth.now = func() time.Time { return now }
	for range 30 {
		if rr := request(s, "POST", "/api/login", `{"username":"no-user","password":"wrong"}`, false); rr.Code != 401 {
			t.Fatalf("login: %d", rr.Code)
		}
	}
	if rr := request(s, "POST", "/api/login", `{"username":"no-user","password":"wrong"}`, false); rr.Code != 429 {
		t.Fatalf("no rate limit: %d", rr.Code)
	}
	s = testServer(t)
	body := `{"url":"https://example.com/` + strings.Repeat("a", 17000) + `"}`
	if rr := request(s, "POST", "/api/shorten", body, true); rr.Code != 400 {
		t.Fatalf("oversized body accepted: %d", rr.Code)
	}
	if rr := request(s, "POST", "/api/shorten", `{"url":"https://example.com"}{}`, true); rr.Code != 400 {
		t.Fatalf("multiple JSON objects accepted: %d", rr.Code)
	}
}
func TestHeadAndUnsafeLegacy(t *testing.T) {
	s := testServer(t)
	link := createTestLink(t, s, "head-test")
	if rr := request(s, "HEAD", "/head-test", "", false); rr.Code != 302 {
		t.Fatalf("HEAD: %d", rr.Code)
	}
	got, err := s.store.Get(context.Background(), link.ShortCode)
	if err != nil || got.Clicks != 0 {
		t.Fatal("HEAD counted as visit")
	}
	// The old course allowed credential-bearing URLs; never redirect or count them.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "LEGACY"), []byte("https://demo:fake@example.com"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.store = st
	if rr := request(s, "GET", "/LEGACY", "", false); rr.Code != 410 {
		t.Fatalf("unsafe legacy redirect: %d", rr.Code)
	}
	got, err = st.Get(context.Background(), "LEGACY")
	if err != nil || got.Clicks != 0 {
		t.Fatal("unsafe legacy counted as visit")
	}
}
func TestLoggerConfiguration(t *testing.T) {
	t.Setenv("ENV", "production")
	if _, _, err := initializeLogger(filepath.Join(t.TempDir(), "absent", "log")); err == nil {
		t.Fatal("invalid log path silently accepted")
	}
	logPath := filepath.Join(t.TempDir(), "app.log")
	logger, closeLogger, err := initializeLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("file test", "password", "must-not-appear")
	if err := closeLogger(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(data)) || bytes.Contains(data, []byte("must-not-appear")) {
		t.Fatalf("file log: %s", data)
	}
}

func TestOversizedPasswordIsClientFailure(t *testing.T) {
	s := testServer(t)
	body, _ := json.Marshal(map[string]string{"username": "frodo", "password": strings.Repeat("a", 73)})
	if rr := request(s, "POST", "/api/login", string(body), false); rr.Code != 401 {
		t.Fatalf("long password produced status %d", rr.Code)
	}
}
func TestRecoveryAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: replaceAttr}))
	handler := requestID(requestLogger(logger)(recoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("do not reveal panic detail") }))))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "bad\x1b[31m")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 500 || rr.Body.String() != "Internal Server Error\n" {
		t.Fatalf("panic response %d %q", rr.Code, rr.Body.String())
	}
	if !validRequestID(rr.Header().Get("X-Request-ID")) {
		t.Fatal("unsafe request ID retained")
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatal("server failure not logged as ERROR")
	}
}
