package web

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"testing"

	"goto/internal/auth"
	"goto/internal/store/memory"
	"goto/internal/testutil/mockoidc"
)

func TestBrowserOIDCFlow(t *testing.T) {
	ctx := context.Background()

	mockOIDC, err := mockoidc.New()
	if err != nil {
		t.Fatalf("failed to create mock OIDC server: %v", err)
	}
	defer mockOIDC.Close()

	sessionSecret := []byte("secret-key-32-bytes-session-test!")
	authHandler, err := auth.New(ctx, auth.Config{
		IssuerURL:     mockOIDC.URL(),
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		BaseURL:       "http://127.0.0.1:8990",
		SessionSecret: sessionSecret,
		SecureCookies: false,
	})
	if err != nil {
		t.Fatalf("failed to initialize auth: %v", err)
	}

	st := memory.New()
	handler := NewRouter(st, authHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:8990")
	if err != nil {
		t.Fatalf("failed to listen on port 8990: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	cmd := exec.Command("node", "../../test/browser_stage4_oidc.mjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser OIDC verification failed: %v\nOutput:\n%s", err, string(out))
	}
	t.Logf("Browser OIDC output:\n%s", string(out))
}
