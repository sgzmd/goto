package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type contextKey string

const (
	UserEmailContextKey contextKey = "user_email"
	CSRFTokenContextKey contextKey = "csrf_token"
	SessionCookieName              = "goto_session"
	OAuthCookieName                = "goto_oidc_flow"

	domainOAuthFlow = "goto_oauth_flow"
	domainSession   = "goto_session"
)

type Config struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	BaseURL       string
	SessionSecret []byte
	SecureCookies bool
}

type Session struct {
	UserID    string `json:"sub"`
	Email     string `json:"email"`
	CSRFToken string `json:"csrf"`
	ExpiresAt int64  `json:"exp"`
}

type OAuthFlowState struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	ReturnTo     string `json:"return_to"`
	ExpiresAt    int64  `json:"exp"`
}

type Authenticator struct {
	provider      *oidc.Provider
	verifier      *oidc.IDTokenVerifier
	oauth2Config  oauth2.Config
	encryptor     *Encryptor
	secureCookies bool
}

func New(ctx context.Context, cfg Config) (*Authenticator, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("OIDC IssuerURL is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("OIDC ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("OIDC ClientSecret is required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("BaseURL is required")
	}

	encryptor, err := NewEncryptor(cfg.SessionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session encryptor: %w", err)
	}

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to discover OIDC provider at %s: %w", cfg.IssuerURL, err)
	}

	redirectURL := strings.TrimRight(cfg.BaseURL, "/") + "/auth/callback"

	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	return &Authenticator{
		provider:      provider,
		verifier:      verifier,
		oauth2Config:  oauth2Config,
		encryptor:     encryptor,
		secureCookies: cfg.SecureCookies,
	}, nil
}

func (a *Authenticator) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state, err := GenerateSecureRandomString(32)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	nonce, err := GenerateSecureRandomString(32)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// PKCE: Generate code_verifier and code_challenge (S256)
	codeVerifier, err := GenerateSecureRandomString(32)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	codeChallengeHash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(codeChallengeHash[:])

	returnTo := SafeReturnURL(r.URL.Query().Get("return_to"), "/admin")

	flow := OAuthFlowState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		ReturnTo:     returnTo,
		ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
	}

	encryptedFlow, err := a.encryptor.EncryptJSON(domainOAuthFlow, flow)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     OAuthCookieName,
		Value:    encryptedFlow,
		Path:     "/auth/callback",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := a.oauth2Config.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *Authenticator) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if idpErr := r.URL.Query().Get("error"); idpErr != "" {
		errDesc := r.URL.Query().Get("error_description")
		http.Error(w, fmt.Sprintf("Authentication failed: %s (%s)", idpErr, errDesc), http.StatusBadRequest)
		return
	}

	cookie, err := r.Cookie(OAuthCookieName)
	if err != nil {
		http.Error(w, "Missing OAuth state cookie", http.StatusBadRequest)
		return
	}

	// Immediately clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthCookieName,
		Value:    "",
		Path:     "/auth/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	var flow OAuthFlowState
	if err := a.encryptor.DecryptJSON(domainOAuthFlow, cookie.Value, &flow); err != nil {
		http.Error(w, "Invalid or tampered OAuth state", http.StatusBadRequest)
		return
	}

	if time.Now().Unix() > flow.ExpiresAt {
		http.Error(w, "OAuth flow has expired", http.StatusBadRequest)
		return
	}

	stateParam := r.URL.Query().Get("state")
	if !ConstantTimeCompare(flow.State, stateParam) {
		http.Error(w, "OAuth state mismatch", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := a.oauth2Config.Exchange(
		r.Context(),
		code,
		oauth2.SetAuthURLParam("code_verifier", flow.CodeVerifier),
	)
	if err != nil {
		http.Error(w, "Failed to exchange authorization code", http.StatusBadRequest)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "Missing id_token in token response", http.StatusBadRequest)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "ID token verification failed", http.StatusBadRequest)
		return
	}

	if idToken.Nonce != flow.Nonce {
		http.Error(w, "Nonce verification failed", http.StatusBadRequest)
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse token claims", http.StatusInternalServerError)
		return
	}

	csrfToken, err := GenerateSecureRandomString(32)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	userEmail := claims.Email
	if userEmail == "" {
		userEmail = claims.Subject
	}

	session := Session{
		UserID:    claims.Subject,
		Email:     userEmail,
		CSRFToken: csrfToken,
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}

	encryptedSession, err := a.encryptor.EncryptJSON(domainSession, session)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    encryptedSession,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
}

func (a *Authenticator) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			a.unauthenticated(w, r)
			return
		}

		var session Session
		if err := a.encryptor.DecryptJSON(domainSession, cookie.Value, &session); err != nil {
			a.unauthenticated(w, r)
			return
		}

		if session.UserID == "" || session.Email == "" || session.CSRFToken == "" {
			a.unauthenticated(w, r)
			return
		}

		if time.Now().Unix() > session.ExpiresAt {
			a.unauthenticated(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), UserEmailContextKey, session.Email)
		ctx = context.WithValue(ctx, CSRFTokenContextKey, session.CSRFToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authenticator) VerifyCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			// Limit body reading to 64KB before parsing form
			r.Body = http.MaxBytesReader(w, r.Body, 1024*64)

			sessionCSRF, _ := r.Context().Value(CSRFTokenContextKey).(string)
			if sessionCSRF == "" {
				http.Error(w, "CSRF validation failed: no session CSRF token", http.StatusForbidden)
				return
			}

			// Do not accept CSRF tokens from URL query parameters (PostFormValue only)
			requestCSRF := r.PostFormValue("csrf_token")
			if requestCSRF == "" {
				requestCSRF = r.Header.Get("X-CSRF-Token")
			}

			if !ConstantTimeCompare(sessionCSRF, requestCSRF) {
				http.Error(w, "CSRF validation failed: token mismatch", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) unauthenticated(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		loginURL := "/auth/login?return_to=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func SafeReturnURL(raw string, defaultURL string) string {
	if raw == "" {
		return defaultURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return defaultURL
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return defaultURL
	}
	if !strings.HasPrefix(raw, "/") {
		return defaultURL
	}
	// Avoid redirect loop back to login or callback
	if strings.HasPrefix(raw, "/auth/login") || strings.HasPrefix(raw, "/auth/callback") {
		return defaultURL
	}
	return raw
}
