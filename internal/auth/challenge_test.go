package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goto/internal/testutil/mockoidc"
)

func TestAuthSecurityAdversarialChallenge(t *testing.T) {
	ctx := context.Background()
	mockOIDC, err := mockoidc.New()
	if err != nil {
		t.Fatalf("failed to start mock OIDC: %v", err)
	}
	defer mockOIDC.Close()

	sessionSecret := []byte("a-very-secure-32-byte-secret-key!")
	cfg := Config{
		IssuerURL:     mockOIDC.URL(),
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		BaseURL:       "http://goto.local",
		SessionSecret: sessionSecret,
		SecureCookies: false,
	}

	auth, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize auth: %v", err)
	}

	encryptor, _ := NewEncryptor(sessionSecret)

	t.Run("Challenge: Nonce Mismatch In ID Token", func(t *testing.T) {
		code := "code-nonce-mismatch"
		mockOIDC.AddAuthCode(code, mockoidc.CodeData{
			Nonce:       "different-nonce-than-cookie",
			Email:       "hacker@example.com",
			Sub:         "user-hacker",
			Code:        code,
			RedirectURI: cfg.BaseURL + "/auth/callback",
		})

		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "original-nonce",
			CodeVerifier: "v-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/callback?code=%s&state=state-1", code), nil)
		req.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec := httptest.NewRecorder()

		auth.HandleCallback(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on nonce mismatch, got %d", rec.Code)
		}
	})

	t.Run("Challenge: Mismatched Audience", func(t *testing.T) {
		code := "code-aud-mismatch"
		mockOIDC.SetOverrideAud("other-client-id")
		mockOIDC.AddAuthCode(code, mockoidc.CodeData{
			Nonce:       "nonce-1",
			Email:       "user@example.com",
			Sub:         "user-1",
			Code:        code,
			RedirectURI: cfg.BaseURL + "/auth/callback",
		})

		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "nonce-1",
			CodeVerifier: "v-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/callback?code=%s&state=state-1", code), nil)
		req.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec := httptest.NewRecorder()

		auth.HandleCallback(rec, req)

		mockOIDC.SetOverrideAud("") // reset

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on audience mismatch, got %d", rec.Code)
		}
	})

	t.Run("Challenge: Malformed ID Token", func(t *testing.T) {
		code := "code-malformed-token"
		mockOIDC.SetInvalidToken(true)
		mockOIDC.AddAuthCode(code, mockoidc.CodeData{
			Nonce:       "nonce-1",
			Email:       "user@example.com",
			Sub:         "user-1",
			Code:        code,
			RedirectURI: cfg.BaseURL + "/auth/callback",
		})

		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "nonce-1",
			CodeVerifier: "v-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/callback?code=%s&state=state-1", code), nil)
		req.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec := httptest.NewRecorder()

		auth.HandleCallback(rec, req)

		mockOIDC.SetInvalidToken(false) // reset

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on malformed token, got %d", rec.Code)
		}
	})

	t.Run("Challenge: Replayed Authorization Code", func(t *testing.T) {
		code := "code-replayed"
		mockOIDC.AddAuthCode(code, mockoidc.CodeData{
			Nonce:       "nonce-1",
			Email:       "user@example.com",
			Sub:         "user-1",
			Code:        code,
			RedirectURI: cfg.BaseURL + "/auth/callback",
		})

		cookieVal, _ := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-1",
			Nonce:        "nonce-1",
			CodeVerifier: "v-1",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})

		// First exchange succeeds
		req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/callback?code=%s&state=state-1", code), nil)
		req1.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec1 := httptest.NewRecorder()
		auth.HandleCallback(rec1, req1)
		if rec1.Code != http.StatusFound {
			t.Fatalf("first exchange failed: %d", rec1.Code)
		}

		// Replay exchange with same code
		req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/callback?code=%s&state=state-1", code), nil)
		req2.AddCookie(&http.Cookie{Name: OAuthCookieName, Value: cookieVal})
		rec2 := httptest.NewRecorder()
		auth.HandleCallback(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on replayed code, got %d", rec2.Code)
		}
	})

	t.Run("Challenge: Cookie Type Confusion / OAuth Flow Token As Session Cookie", func(t *testing.T) {
		// An unauthenticated user generates a valid OAuth flow token
		flowToken, err := encryptor.EncryptJSON(domainOAuthFlow, OAuthFlowState{
			State:        "state-xyz",
			Nonce:        "nonce-xyz",
			CodeVerifier: "code-verifier-xyz",
			ReturnTo:     "/admin",
			ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
		})
		if err != nil {
			t.Fatalf("failed to create flow token: %v", err)
		}

		// Attacker attempts to pass this flow token as a session cookie
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: flowToken})
		rec := httptest.NewRecorder()

		handler := auth.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		handler.ServeHTTP(rec, req)

		// Must be rejected as unauthenticated (redirect to login 302, NOT 200 OK)
		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 redirect to login on type confusion attack, got %d", rec.Code)
		}
	})
}
