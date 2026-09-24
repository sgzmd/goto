package web

import (
	"net/http"

	"goto/internal/auth"
	"goto/internal/link"
)

func NewRouter(store link.Store, authHandler *auth.Authenticator) http.Handler {
	mux := http.NewServeMux()

	admin := NewAdminHandler(store)
	resolver := NewResolver(store)

	// Health check (ServeMux automatically routes HEAD to GET)
	mux.HandleFunc("GET /healthz", HandleHealthz)

	// Root path redirects to /admin
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusFound)
	})

	if authHandler != nil {
		mux.HandleFunc("GET /auth/login", authHandler.HandleLogin)
		mux.HandleFunc("GET /auth/callback", authHandler.HandleCallback)
		// Logout can be called unconditionally to clear session cookie
		mux.HandleFunc("POST /auth/logout", authHandler.HandleLogout)

		// Admin dashboard
		mux.Handle("GET /admin", authHandler.RequireAuth(http.HandlerFunc(admin.HandleDashboard)))

		// Admin mutations protected by auth + CSRF
		mux.Handle("POST /admin/links", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleCreateLink))))
		mux.Handle("POST /admin/links/edit", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleEditLink))))
		mux.Handle("POST /admin/links/delete", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleDeleteLink))))
	} else {
		mux.HandleFunc("GET /admin", admin.HandleDashboard)
		mux.HandleFunc("POST /admin/links", admin.HandleCreateLink)
		mux.HandleFunc("POST /admin/links/edit", admin.HandleEditLink)
		mux.HandleFunc("POST /admin/links/delete", admin.HandleDeleteLink)
	}

	// Short link resolver
	mux.HandleFunc("GET /{slug}", resolver.HandleResolve)

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
