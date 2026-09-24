package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestAuthSecurityLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	origLogger := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(origLogger)

	authHandler := &Authenticator{
		secureCookies: false,
	}

	t.Run("Logs unauthenticated access to protected resource at DEBUG", func(t *testing.T) {
		buf.Reset()
		dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		protected := authHandler.RequireAuth(dummy)

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected redirect to login (302), got %d", rec.Code)
		}

		entries := parseLogs(&buf)
		log, found := findLog(entries, "Protected endpoint access without session cookie")
		if !found {
			t.Fatalf("expected 'Protected endpoint access without session cookie' log, got:\n%s", buf.String())
		}
		if log["level"] != "DEBUG" {
			t.Errorf("expected level DEBUG, got %v", log["level"])
		}
		if log["path"] != "/admin" {
			t.Errorf("expected path /admin, got %v", log["path"])
		}
	})

	t.Run("Logs CSRF failure at WARN", func(t *testing.T) {
		buf.Reset()
		dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		csrfProtected := authHandler.VerifyCSRF(dummy)

		// Context has session CSRF token, but POST body does not provide matching token
		ctx := context.WithValue(context.Background(), CSRFTokenContextKey, "valid-secret-token")
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader("slug=test&csrf_token=wrong-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		csrfProtected.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
		}

		entries := parseLogs(&buf)
		log, found := findLog(entries, "CSRF validation failed: token mismatch")
		if !found {
			t.Fatalf("expected 'CSRF validation failed: token mismatch' log, got:\n%s", buf.String())
		}
		if log["level"] != "WARN" {
			t.Errorf("expected level WARN, got %v", log["level"])
		}
		if log["path"] != "/admin/links" {
			t.Errorf("expected path /admin/links, got %v", log["path"])
		}
	})
}
