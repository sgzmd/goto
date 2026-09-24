package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"goto/internal/store/memory"
)

func TestAdminHandler(t *testing.T) {
	st := memory.New()
	admin := NewAdminHandler(st)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin", admin.HandleDashboard)
	mux.HandleFunc("POST /admin/links", admin.HandleCreateLink)
	mux.HandleFunc("POST /admin/links/edit", admin.HandleEditLink)
	mux.HandleFunc("POST /admin/links/delete", admin.HandleDeleteLink)

	t.Run("Dashboard GET empty list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "No short links created yet") {
			t.Errorf("expected empty state message, got: %s", body)
		}
	})

	t.Run("Create link success", func(t *testing.T) {
		form := url.Values{
			"slug":   {"wiki"},
			"target": {"https://wikipedia.org"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "success=") {
			t.Errorf("expected success redirect, got %s", loc)
		}

		// Verify in store
		l, err := st.Get(context.Background(), "wiki")
		if err != nil {
			t.Fatalf("link not created in store: %v", err)
		}
		if l.Target != "https://wikipedia.org" {
			t.Errorf("got target %s, want https://wikipedia.org", l.Target)
		}
	})

	t.Run("Create link duplicate slug", func(t *testing.T) {
		form := url.Values{
			"slug":   {"wiki"},
			"target": {"https://another.org"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "error=") || !strings.Contains(loc, "already+exists") {
			t.Errorf("expected duplicate error redirect, got %s", loc)
		}
	})

	t.Run("Create link invalid slug", func(t *testing.T) {
		form := url.Values{
			"slug":   {"invalid/slash"},
			"target": {"https://example.com"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error redirect, got %s", loc)
		}
	})

	t.Run("Create link invalid target URL", func(t *testing.T) {
		form := url.Values{
			"slug":   {"badurl"},
			"target": {"javascript:alert(1)"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error redirect, got %s", loc)
		}
	})

	t.Run("Edit link target success", func(t *testing.T) {
		form := url.Values{
			"slug":   {"wiki"},
			"target": {"https://en.wikipedia.org/wiki/Main_Page"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links/edit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}

		l, err := st.Get(context.Background(), "wiki")
		if err != nil {
			t.Fatalf("link fetch failed: %v", err)
		}
		if l.Target != "https://en.wikipedia.org/wiki/Main_Page" {
			t.Errorf("got target %s, want https://en.wikipedia.org/wiki/Main_Page", l.Target)
		}
	})

	t.Run("Edit non-existent link", func(t *testing.T) {
		form := url.Values{
			"slug":   {"notfound"},
			"target": {"https://example.com"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links/edit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "error=Link+not+found") {
			t.Errorf("expected not found error, got %s", loc)
		}
	})

	t.Run("HTML escaping in dashboard", func(t *testing.T) {
		// Verify template safely escapes HTML characters
		req := httptest.NewRequest(http.MethodGet, "/admin?error=<script>alert(1)</script>", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		body := rec.Body.String()
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatalf("unescaped script tag found in HTML response: %s", body)
		}
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("expected escaped script in body, got: %s", body)
		}
	})

	t.Run("Delete link success", func(t *testing.T) {
		form := url.Values{
			"slug": {"wiki"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/links/delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther, got %d", rec.Code)
		}

		_, err := st.Get(context.Background(), "wiki")
		if err == nil {
			t.Fatalf("expected link to be deleted, but still found")
		}
	})

	t.Run("Method not allowed checks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/admin", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for PUT /admin, got %d", rec.Code)
		}

		req = httptest.NewRequest(http.MethodGet, "/admin/links", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for GET /admin/links, got %d", rec.Code)
		}
	})
}
