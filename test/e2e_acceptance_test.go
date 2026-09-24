package test

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"goto/internal/auth"
	"goto/internal/store/sqlite"
	"goto/internal/testutil/mockoidc"
	"goto/internal/web"
)

func TestEndToEndBrowserAcceptance(t *testing.T) {
	ctx := context.Background()

	mockOIDC, err := mockoidc.New()
	if err != nil {
		t.Fatalf("failed to start mock OIDC server: %v", err)
	}
	defer mockOIDC.Close()

	sessionSecret := []byte("a-very-long-secret-key-that-is-32-bytes!")
	authHandler, err := auth.New(ctx, auth.Config{
		IssuerURL:     mockOIDC.URL(),
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		BaseURL:       "http://127.0.0.1:8995",
		SessionSecret: sessionSecret,
		SecureCookies: false,
	})
	if err != nil {
		t.Fatalf("failed to initialize auth handler: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "e2e_clean.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	router := web.NewRouter(store, authHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:8995")
	if err != nil {
		t.Fatalf("failed to listen on 127.0.0.1:8995: %v", err)
	}
	server := &http.Server{Handler: router}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	cmd := exec.Command("node", "browser_stage7_e2e.mjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Stage 7 browser acceptance test failed: %v\nOutput:\n%s", err, string(out))
	}
	t.Logf("Stage 7 browser acceptance output:\n%s", string(out))
}
