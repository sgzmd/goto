package web

import (
	"errors"
	"log/slog"
	"net/http"

	"goto/internal/link"
)

type Resolver struct {
	store link.Store
}

func NewResolver(store link.Store) *Resolver {
	return &Resolver{store: store}
}

func (h *Resolver) HandleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		slog.Warn("Method not allowed on resolve endpoint", "method", r.Method, "remote_addr", r.RemoteAddr)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := r.PathValue("slug")
	cleanSlug, err := link.ValidateSlug(slug)
	if err != nil {
		slog.Warn("Invalid slug format requested", "slug", slug, "error", err, "remote_addr", r.RemoteAddr)
		http.NotFound(w, r)
		return
	}

	l, err := h.store.Get(r.Context(), cleanSlug)
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			slog.Info("Short link not found", "slug", cleanSlug, "remote_addr", r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
		slog.Error("Database error during link resolution", "slug", cleanSlug, "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	slog.Info("Short link resolved", "slug", cleanSlug, "target", l.Target, "remote_addr", r.RemoteAddr)
	http.Redirect(w, r, l.Target, http.StatusFound)
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
