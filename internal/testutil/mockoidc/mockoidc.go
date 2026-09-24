package mockoidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type Server struct {
	server       *httptest.Server
	privKey      *rsa.PrivateKey
	keyID        string
	mu           sync.Mutex
	authCodes    map[string]CodeData
	overrideAud  string
	overrideIss  string
	invalidToken bool
}

type CodeData struct {
	Nonce       string
	Email       string
	Sub         string
	Code        string
	RedirectURI string
}

func New() (*Server, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	mock := &Server{
		privKey:   privKey,
		keyID:     "mock-key-1",
		authCodes: make(map[string]CodeData),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", mock.handleDiscovery)
	mux.HandleFunc("GET /jwks.json", mock.handleJWKS)
	mux.HandleFunc("GET /authorize", mock.handleAuthorize)
	mux.HandleFunc("POST /authorize/submit", mock.handleAuthorizeSubmit)
	mux.HandleFunc("POST /token", mock.handleToken)

	mock.server = httptest.NewServer(mux)
	return mock, nil
}

func NewWithAddr(addr string, publicURL string) (*Server, http.Handler, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	mock := &Server{
		privKey:     privKey,
		keyID:       "mock-key-1",
		authCodes:   make(map[string]CodeData),
		overrideIss: publicURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", mock.handleDiscovery)
	mux.HandleFunc("GET /jwks.json", mock.handleJWKS)
	mux.HandleFunc("GET /authorize", mock.handleAuthorize)
	mux.HandleFunc("POST /authorize/submit", mock.handleAuthorizeSubmit)
	mux.HandleFunc("POST /token", mock.handleToken)

	return mock, mux, nil
}

func (m *Server) Close() {
	m.server.Close()
}

func (m *Server) URL() string {
	return m.server.URL
}

func (m *Server) SetOverrideAud(aud string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrideAud = aud
}

func (m *Server) SetInvalidToken(invalid bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidToken = invalid
}

func (m *Server) AddAuthCode(code string, data CodeData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authCodes[code] = data
}

func (m *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	iss := m.overrideIss
	if iss == "" && m.server != nil {
		iss = m.server.URL
	}
	resp := map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/authorize",
		"token_endpoint":                        iss + "/token",
		"jwks_uri":                              iss + "/jwks.json",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (m *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwk := jose.JSONWebKey{
		Key:       &m.privKey.PublicKey,
		KeyID:     m.keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	keySet := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{jwk},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keySet)
}

func (m *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	nonce := r.URL.Query().Get("nonce")

	code := fmt.Sprintf("code-%d", time.Now().UnixNano())
	m.mu.Lock()
	m.authCodes[code] = CodeData{
		Nonce:       nonce,
		Email:       "engineer@example.com",
		Sub:         "user-12345",
		Code:        code,
		RedirectURI: redirectURI,
	}
	m.mu.Unlock()

	if r.URL.Query().Get("auto") == "1" {
		target := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, state)
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Mock OIDC Login</title></head>
<body>
    <h2>Mock Identity Provider (Okta / Google Simulator)</h2>
    <form method="POST" action="/authorize/submit">
        <input type="hidden" name="code" value="%s">
        <input type="hidden" name="state" value="%s">
        <input type="hidden" name="redirect_uri" value="%s">
        <label>Sign in as: <input type="email" name="email" value="engineer@example.com"></label><br><br>
        <button id="login-submit" type="submit">Sign In</button>
    </form>
</body>
</html>`, code, state, redirectURI)
	_, _ = w.Write([]byte(html))
}

func (m *Server) handleAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := r.FormValue("code")
	state := r.FormValue("state")
	redirectURI := r.FormValue("redirect_uri")
	email := r.FormValue("email")

	m.mu.Lock()
	if data, ok := m.authCodes[code]; ok {
		if email != "" {
			data.Email = email
		}
		m.authCodes[code] = data
	}
	m.mu.Unlock()

	target := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, state)
	http.Redirect(w, r, target, http.StatusFound)
}

func (m *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_ = r.ParseForm()
	code := r.FormValue("code")

	m.mu.Lock()
	codeData, ok := m.authCodes[code]
	delete(m.authCodes, code)
	invalid := m.invalidToken
	m.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	if invalid {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"dummy","id_token":"invalid.jwt.token"}`))
		return
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.privKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", m.keyID),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	iss := m.overrideIss
	if iss == "" && m.server != nil {
		iss = m.server.URL
	}
	aud := "test-client-id"
	if m.overrideAud != "" {
		aud = m.overrideAud
	}

	claims := map[string]any{
		"iss":   iss,
		"sub":   codeData.Sub,
		"aud":   aud,
		"exp":   now.Add(1 * time.Hour).Unix(),
		"iat":   now.Unix(),
		"email": codeData.Email,
		"nonce": codeData.Nonce,
	}

	rawJWT, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     rawJWT,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
