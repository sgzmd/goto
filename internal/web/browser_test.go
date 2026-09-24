package web

import (
	"net"
	"net/http"
	"os/exec"
	"testing"

	"goto/internal/store/memory"
)

func TestBrowserUIFlow(t *testing.T) {
	st := memory.New()
	admin := NewAdminHandler(st)
	resolver := NewResolver(st)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HandleHealthz)
	mux.HandleFunc("GET /admin", admin.HandleDashboard)
	mux.HandleFunc("POST /admin/links", admin.HandleCreateLink)
	mux.HandleFunc("POST /admin/links/edit", admin.HandleEditLink)
	mux.HandleFunc("POST /admin/links/delete", admin.HandleDeleteLink)
	mux.HandleFunc("GET /{slug}", resolver.HandleResolve)

	listener, err := net.Listen("tcp", "127.0.0.1:8989")
	if err != nil {
		t.Fatalf("failed to listen on port 8989: %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	cmd := exec.Command("node", "../../test/browser_stage3.mjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser verification failed: %v\nOutput:\n%s", err, string(out))
	}
	t.Logf("Browser test output:\n%s", string(out))
}
