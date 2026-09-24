package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goto/internal/auth"
	"goto/internal/link"
	"goto/internal/store/memory"
)

type logEntry map[string]any

func parseLogs(buf *bytes.Buffer) []logEntry {
	var entries []logEntry
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func findLog(entries []logEntry, msg string) (logEntry, bool) {
	for _, e := range entries {
		if m, ok := e["msg"].(string); ok && m == msg {
			return e, true
		}
	}
	return nil, false
}

func TestHTTPLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	origLogger := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(origLogger)

	st := memory.New()
	_ = st.Create(context.Background(), &link.Link{Slug: "go", Target: "https://golang.org"})
	router := NewRouter(st, nil)

	t.Run("Logs request completion for link resolution", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, "/go", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", rec.Code)
		}

		entries := parseLogs(&buf)
		// Check for resolve specific log
		resolveLog, found := findLog(entries, "Short link resolved")
		if !found {
			t.Fatalf("expected 'Short link resolved' log, got logs:\n%s", buf.String())
		}
		if resolveLog["slug"] != "go" {
			t.Errorf("expected slug 'go', got %v", resolveLog["slug"])
		}
		if resolveLog["target"] != "https://golang.org" {
			t.Errorf("expected target 'https://golang.org', got %v", resolveLog["target"])
		}

		// Check for HTTP request completed log
		httpLog, found := findLog(entries, "HTTP request completed")
		if !found {
			t.Fatalf("expected 'HTTP request completed' log, got logs:\n%s", buf.String())
		}
		if httpLog["method"] != "GET" {
			t.Errorf("expected method GET, got %v", httpLog["method"])
		}
		if httpLog["path"] != "/go" {
			t.Errorf("expected path /go, got %v", httpLog["path"])
		}
		if int(httpLog["status"].(float64)) != http.StatusFound {
			t.Errorf("expected status 302, got %v", httpLog["status"])
		}
		if httpLog["level"] != "INFO" {
			t.Errorf("expected level INFO, got %v", httpLog["level"])
		}
	})

	t.Run("Logs 404 for unknown link at WARN/INFO level", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		entries := parseLogs(&buf)
		_, found := findLog(entries, "Short link not found")
		if !found {
			t.Fatalf("expected 'Short link not found' log, got logs:\n%s", buf.String())
		}

		httpLog, found := findLog(entries, "HTTP request completed")
		if !found {
			t.Fatalf("expected 'HTTP request completed' log, got logs:\n%s", buf.String())
		}
		if int(httpLog["status"].(float64)) != http.StatusNotFound {
			t.Errorf("expected status 404, got %v", httpLog["status"])
		}
		if httpLog["level"] != "WARN" {
			t.Errorf("expected level WARN for 404, got %v", httpLog["level"])
		}
	})

	t.Run("Logs /healthz at DEBUG level", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		entries := parseLogs(&buf)
		httpLog, found := findLog(entries, "HTTP request completed")
		if !found {
			t.Fatalf("expected 'HTTP request completed' log, got logs:\n%s", buf.String())
		}
		if httpLog["level"] != "DEBUG" {
			t.Errorf("expected level DEBUG for /healthz, got %v", httpLog["level"])
		}
	})

	t.Run("Logs admin link mutation with user email", func(t *testing.T) {
		buf.Reset()
		form := url.Values{
			"slug":   {"docs"},
			"target": {"https://docs.example.com"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Simulate authenticated context
		ctx := context.WithValue(req.Context(), auth.UserEmailContextKey, "admin@example.com")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		entries := parseLogs(&buf)
		createLog, found := findLog(entries, "Link created successfully")
		if !found {
			t.Fatalf("expected 'Link created successfully' log, got logs:\n%s", buf.String())
		}
		if createLog["slug"] != "docs" {
			t.Errorf("expected slug docs, got %v", createLog["slug"])
		}
		if createLog["user"] != "admin@example.com" {
			t.Errorf("expected user admin@example.com, got %v", createLog["user"])
		}

		httpLog, found := findLog(entries, "HTTP request completed")
		if !found {
			t.Fatalf("expected 'HTTP request completed' log, got logs:\n%s", buf.String())
		}
		if httpLog["user"] != "admin@example.com" {
			t.Errorf("expected HTTP log user admin@example.com, got %v", httpLog["user"])
		}
	})

	t.Run("Logs conflict warning when creating existing slug", func(t *testing.T) {
		buf.Reset()
		form := url.Values{
			"slug":   {"go"},
			"target": {"https://other.com"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserEmailContextKey, "operator@example.com")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		entries := parseLogs(&buf)
		conflictLog, found := findLog(entries, "Link creation conflict: slug exists")
		if !found {
			t.Fatalf("expected conflict log, got:\n%s", buf.String())
		}
		if conflictLog["slug"] != "go" {
			t.Errorf("expected slug 'go', got %v", conflictLog["slug"])
		}
		if conflictLog["user"] != "operator@example.com" {
			t.Errorf("expected user operator@example.com, got %v", conflictLog["user"])
		}
	})
}
