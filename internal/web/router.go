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

	mux.HandleFunc("GET /healthz", HandleHealthz)

	if authHandler != nil {
		mux.HandleFunc("GET /auth/login", authHandler.HandleLogin)
		mux.HandleFunc("GET /auth/callback", authHandler.HandleCallback)
		mux.Handle("POST /auth/logout", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(authHandler.HandleLogout))))

		// Admin dashboard
		mux.Handle("GET /admin", authHandler.RequireAuth(http.HandlerFunc(admin.HandleDashboard)))

		// Admin mutations
		mux.Handle("POST /admin/links", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleCreateLink))))
		mux.Handle("POST /admin/links/edit", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleEditLink))))
		mux.Handle("POST /admin/links/delete", authHandler.RequireAuth(authHandler.VerifyCSRF(http.HandlerFunc(admin.HandleDeleteLink))))
	} else {
		mux.HandleFunc("GET /admin", admin.HandleDashboard)
		mux.HandleFunc("POST /admin/links", admin.HandleCreateLink)
		mux.HandleFunc("POST /admin/links/edit", admin.HandleEditLink)
		mux.HandleFunc("POST /admin/links/delete", admin.HandleDeleteLink)
	}

	mux.HandleFunc("GET /{slug}", resolver.HandleResolve)

	return mux
}
