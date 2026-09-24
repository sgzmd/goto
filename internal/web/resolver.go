package web

import (
	"errors"
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := r.PathValue("slug")
	cleanSlug, err := link.ValidateSlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	l, err := h.store.Get(r.Context(), cleanSlug)
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, l.Target, http.StatusFound)
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
