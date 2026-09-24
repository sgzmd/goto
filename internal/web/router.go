package web

import (
	"log/slog"
	"net/http"
	"time"

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

	return loggingMiddleware(securityHeaders(mux))
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += int64(n)
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.statusCode,
			"duration_ms", duration.Milliseconds(),
			"bytes", rec.bytesWritten,
			"remote_addr", r.RemoteAddr,
		}

		if email, ok := r.Context().Value(auth.UserEmailContextKey).(string); ok && email != "" {
			attrs = append(attrs, "user", email)
		}

		msg := "HTTP request completed"
		if r.URL.Path == "/healthz" {
			slog.Debug(msg, attrs...)
		} else if rec.statusCode >= 500 {
			slog.Error(msg, attrs...)
		} else if rec.statusCode >= 400 {
			slog.Warn(msg, attrs...)
		} else {
			slog.Info(msg, attrs...)
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
