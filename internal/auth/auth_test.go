package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"goto/internal/testutil/mockoidc"
)

func TestAuthSecurityAndFlow(t *testing.T) {
	ctx := context.Background()
	mockOIDC, err := mockoidc.New()
	if err != nil {
		t.Fatalf("failed to start mock OIDC server: %v", err)
	}
	defer mockOIDC.Close()

	sessionSecret := []byte("super-secret-key-that-is-32-bytes!!")
	cfg := Config{
		IssuerURL:     mockOIDC.URL(),
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		BaseURL:       "http://goto.example.com",
		SessionSecret: sessionSecret,
		SecureCookies: false,
	}

	auth, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	adminHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, _ := r.Context().Value(UserEmailContextKey).(string)
		csrf, _ := r.Context().Value(CSRFTokenContextKey).(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf("OK:%s:%s", email, csrf)))
	})

	mutationHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("MUTATED"))
	})

	protectedAdmin := auth.RequireAuth(adminHandler)
	protectedMutation := auth.RequireAuth(auth.VerifyCSRF(mutationHandler))

	t.Run("Unauthenticated GET redirects to login with return_to", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin?filter=test", nil)
		rec := httptest.NewRecorder()

		protectedAdmin.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 redirect, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/auth/login?return_to=") {
			t.Fatalf("unexpected redirect location: %s", loc)
		}
		if !strings.Contains(loc, url.QueryEscape("/admin?filter=test")) {
			t.Errorf("expected return_to to contain target path, got %s", loc)
		}
	})

	t.Run("Unauthenticated POST returns 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/links", nil)
		rec := httptest.NewRecorder()

		protectedMutation.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for unauthenticated mutation, got %d", rec.Code)
		}
	})

	t.Run("SafeReturnURL prevention of open redirects", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"/admin", "/admin"},
			{"/admin?tab=links", "/admin?tab=links"},
			{"https://evil.com/steal", "/admin"},
			{"http://evil.com", "/admin"},
			{"//evil.com", "/admin"},
			{"/\\evil.com", "/admin"},
			{"javascript:alert(1)", "/admin"},
			{"", "/admin"},
		}
		for _, tt := range tests {
			got := SafeReturnURL(tt.input, "/admin")
			if got != tt.want {
				t.Errorf("SafeReturnURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		}
	})

	var validSessionCookie *http.Cookie
	var validCSRFToken string

	t.Run("Full login handshake to session creation", func(t *testing.T) {
		// 1. Visit /auth/login?return_to=/admin
		loginReq := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/admin", nil)
		loginRec := httptest.NewRecorder()

		auth.HandleLogin(loginRec, loginReq)

		if loginRec.Code != http.StatusFound {
			t.Fatalf("expected 302 from HandleLogin, got %d", loginRec.Code)
		}

		cookies := loginRec.Result().Cookies()
		var stateCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == OAuthCookieName {
				stateCookie = c
				break
			}
		}
		if stateCookie == nil {
			t.Fatalf("expected %s cookie to be set", OAuthCookieName)
		}

		authURLStr := loginRec.Header().Get("Location")
		authURL, err := url.Parse(authURLStr)
		if err != nil {
			t.Fatalf("failed to parse auth URL: %v", err)
		}

		state := authURL.Query().Get("state")
		nonce := authURL.Query().Get("nonce")
		codeChallenge := authURL.Query().Get("code_challenge")
		if state == "" || nonce == "" || codeChallenge == "" {
			t.Fatalf("missing required oauth params in auth URL: %s", authURLStr)
		}

		// Simulate IdP issuing authorization code
		code := "test-auth-code-1"
		mockOIDC.AddAuthCode(code, mockoidc.CodeData{
			Nonce:       nonce,
			Email:       "alice@example.com",
			Sub:         "user-alice",
			Code:        code,
			RedirectURI: cfg.BaseURL + "/auth/callback",
		})

		// 2. Callback
		callbackURL := fmt.Sprintf("/auth/callback?code=%s&state=%s", code, state)
		cbReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
		cbReq.AddCookie(stateCookie)
		cbRec := httptest.NewRecorder()

		auth.HandleCallback(cbRec, cbReq)

		if cbRec.Code != http.StatusFound {
			t.Fatalf("expected 302 from callback, got %d. Body: %s", cbRec.Code, cbRec.Body.String())
		}
		if cbRec.Header().Get("Location") != "/admin" {
			t.Errorf("expected redirect to /admin, got %s", cbRec.Header().Get("Location"))
		}

		for _, c := range cbRec.Result().Cookies() {
			if c.Name == SessionCookieName && c.Value != "" {
				validSessionCookie = c
			}
		}
		if validSessionCookie == nil {
			t.Fatalf("expected session cookie to be set")
		}

		// 3. Verify session works and grants access
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(validSessionCookie)
		rec := httptest.NewRecorder()

		protectedAdmin.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with valid session, got %d", rec.Code)
		}
		body := rec.Body.String()
		parts := strings.Split(body, ":")
		if len(parts) != 3 || parts[1] != "alice@example.com" {
			t.Fatalf("unexpected body: %s", body)
		}
		validCSRFToken = parts[2]
	})

	t.Run("Session tampering is detected and rejected", func(t *testing.T) {
		tampered := *validSessionCookie
		// Flip one character in the base64 token
		valBytes := []byte(tampered.Value)
		if valBytes[10] == 'A' {
			valBytes[10] = 'B'
		} else {
			valBytes[10] = 'A'
		}
		tampered.Value = string(valBytes)

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(&tampered)
		rec := httptest.NewRecorder()

		protectedAdmin.ServeHTTP(rec, req)

		// Tampered session cookie must be treated as unauthenticated (302 to login)
		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 to login on tampered session, got %d", rec.Code)
		}
	})

	t.Run("CSRF validation on mutations", func(t *testing.T) {
		// 1. Missing CSRF token
		req := httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader("slug=test&target=https://example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(validSessionCookie)
		rec := httptest.NewRecorder()

		protectedMutation.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for missing CSRF, got %d", rec.Code)
		}

		// 2. Invalid CSRF token
		form := url.Values{"csrf_token": {"wrong-token"}}
		req = httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(validSessionCookie)
		rec = httptest.NewRecorder()

		protectedMutation.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for invalid CSRF, got %d", rec.Code)
		}

		// 3. Valid CSRF token
		form = url.Values{"csrf_token": {validCSRFToken}}
		req = httptest.NewRequest(http.MethodPost, "/admin/links", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(validSessionCookie)
		rec = httptest.NewRecorder()

		protectedMutation.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for valid CSRF, got %d", rec.Code)
		}
	})

	t.Run("Callback rejects mismatched state", func(t *testing.T) {
		// Valid state in cookie
		encryptor, _ := NewEncryptor(sessionSecret)
		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "nonce-1",
			CodeVerifier: "verifier-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=somecode&state=wrong-state", nil)
		req.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec := httptest.NewRecorder()

		auth.HandleCallback(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on state mismatch, got %d", rec.Code)
		}
	})

	t.Run("Callback rejects expired state", func(t *testing.T) {
		encryptor, _ := NewEncryptor(sessionSecret)
		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "nonce-1",
			CodeVerifier: "verifier-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(-5 * time.Minute).Unix(), // expired
		})

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=somecode&state=state-1", nil)
		req.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec := httptest.NewRecorder()

		auth.HandleCallback(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on expired flow, got %d", rec.Code)
		}
	})

	t.Run("Logout clears session cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(validSessionCookie)
		rec := httptest.NewRecorder()

		auth.HandleLogout(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 SeeOther from logout, got %d", rec.Code)
		}
		var cleared bool
		for _, c := range rec.Result().Cookies() {
			if c.Name == SessionCookieName && c.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Fatalf("expected session cookie to be cleared with negative MaxAge")
		}
	})
}
