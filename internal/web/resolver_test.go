package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"goto/internal/link"
	"goto/internal/store/memory"
)

func TestHandleResolve(t *testing.T) {
	st := memory.New()
	_ = st.Create(context.Background(), &link.Link{
		Slug:   "wiki",
		Target: "https://wikipedia.org",
	})

	resolver := NewResolver(st)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{slug}", resolver.HandleResolve)

	tests := []struct {
		name           string
		path           string
		method         string
		wantStatusCode int
		wantLocation   string
	}{
		{
			name:           "existing link redirects",
			path:           "/wiki",
			method:         http.MethodGet,
			wantStatusCode: http.StatusFound,
			wantLocation:   "https://wikipedia.org",
		},
		{
			name:           "existing link case insensitive redirects",
			path:           "/WIKI",
			method:         http.MethodGet,
			wantStatusCode: http.StatusFound,
			wantLocation:   "https://wikipedia.org",
		},
		{
			name:           "unknown link returns 404",
			path:           "/missing",
			method:         http.MethodGet,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "invalid slug characters return 404",
			path:           "/invalid*slug",
			method:         http.MethodGet,
			wantStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatusCode)
			}
			if tt.wantLocation != "" {
				loc := rec.Header().Get("Location")
				if loc != tt.wantLocation {
					t.Errorf("got Location %q, want %q", loc, tt.wantLocation)
				}
			}
		})
	}
}

func TestHandleHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	HandleHealthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok\n" {
		t.Errorf("got body %q, want %q", rec.Body.String(), "ok\n")
	}
}
