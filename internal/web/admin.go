package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"goto/internal/auth"
	"goto/internal/link"
)

//go:embed templates/*
var templateFS embed.FS

var adminTmpl = template.Must(template.ParseFS(templateFS, "templates/index.html"))

type AdminHandler struct {
	store link.Store
}

type AdminViewData struct {
	Links          []*link.Link
	UserEmail      string
	CSRFToken      string
	ErrorMessage   string
	SuccessMessage string
	FormSlug       string
	FormTarget     string
}

func NewAdminHandler(store link.Store) *AdminHandler {
	return &AdminHandler{store: store}
}

func (h *AdminHandler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	links, err := h.store.List(r.Context())
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := AdminViewData{
		Links:          links,
		ErrorMessage:   r.URL.Query().Get("error"),
		SuccessMessage: r.URL.Query().Get("success"),
	}

	// Session/CSRF context will populate these if auth middleware is present
	if email, ok := r.Context().Value(auth.UserEmailContextKey).(string); ok {
		data.UserEmail = email
	}
	if csrf, ok := r.Context().Value(auth.CSRFTokenContextKey).(string); ok {
		data.CSRFToken = csrf
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.Execute(w, data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func (h *AdminHandler) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	rawSlug := r.FormValue("slug")
	rawTarget := r.FormValue("target")

	cleanSlug, err := link.ValidateSlug(rawSlug)
	if err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	cleanTarget, err := link.ValidateTargetURL(rawTarget)
	if err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	err = h.store.Create(r.Context(), &link.Link{
		Slug:   cleanSlug,
		Target: cleanTarget,
	})
	if err != nil {
		if errors.Is(err, link.ErrConflict) {
			http.Redirect(w, r, "/admin?error="+url.QueryEscape(fmt.Sprintf("Slug %q already exists", cleanSlug)), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to save link"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Link /%s created", cleanSlug)), http.StatusSeeOther)
}

func (h *AdminHandler) HandleEditLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	cleanSlug, err := link.ValidateSlug(r.FormValue("slug"))
	if err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Invalid slug"), http.StatusSeeOther)
		return
	}

	cleanTarget, err := link.ValidateTargetURL(r.FormValue("target"))
	if err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	err = h.store.Update(r.Context(), &link.Link{
		Slug:   cleanSlug,
		Target: cleanTarget,
	})
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			http.Redirect(w, r, "/admin?error="+url.QueryEscape("Link not found"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to update link"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Target for /%s updated", cleanSlug)), http.StatusSeeOther)
}

func (h *AdminHandler) HandleDeleteLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	cleanSlug, err := link.ValidateSlug(r.FormValue("slug"))
	if err != nil {
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Invalid slug"), http.StatusSeeOther)
		return
	}

	err = h.store.Delete(r.Context(), cleanSlug)
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			http.Redirect(w, r, "/admin?error="+url.QueryEscape("Link not found"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to delete link"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Link /%s deleted", cleanSlug)), http.StatusSeeOther)
}
