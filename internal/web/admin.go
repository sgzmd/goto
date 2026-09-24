package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	links, err := h.store.List(r.Context())
	if err != nil {
		slog.Error("Failed to list links for dashboard", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := AdminViewData{
		Links:          links,
		ErrorMessage:   r.URL.Query().Get("error"),
		SuccessMessage: r.URL.Query().Get("success"),
		FormSlug:       r.URL.Query().Get("form_slug"),
		FormTarget:     r.URL.Query().Get("form_target"),
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
		slog.Error("Failed to render admin dashboard template", "error", err)
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func (h *AdminHandler) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userEmail, _ := r.Context().Value(auth.UserEmailContextKey).(string)

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		slog.Warn("Failed to parse link creation form", "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	rawSlug := r.FormValue("slug")
	rawTarget := r.FormValue("target")

	cleanSlug, err := link.ValidateSlug(rawSlug)
	if err != nil {
		slog.Warn("Link creation rejected: invalid slug", "raw_slug", rawSlug, "error", err, "user", userEmail)
		errURL := fmt.Sprintf("/admin?error=%s&form_slug=%s&form_target=%s",
			url.QueryEscape(err.Error()), url.QueryEscape(rawSlug), url.QueryEscape(rawTarget))
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}

	cleanTarget, err := link.ValidateTargetURL(rawTarget)
	if err != nil {
		slog.Warn("Link creation rejected: invalid target URL", "raw_target", rawTarget, "error", err, "user", userEmail)
		errURL := fmt.Sprintf("/admin?error=%s&form_slug=%s&form_target=%s",
			url.QueryEscape(err.Error()), url.QueryEscape(rawSlug), url.QueryEscape(rawTarget))
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}

	err = h.store.Create(r.Context(), &link.Link{
		Slug:   cleanSlug,
		Target: cleanTarget,
	})
	if err != nil {
		if errors.Is(err, link.ErrConflict) {
			slog.Warn("Link creation conflict: slug exists", "slug", cleanSlug, "user", userEmail)
			errURL := fmt.Sprintf("/admin?error=%s&form_slug=%s&form_target=%s",
				url.QueryEscape(fmt.Sprintf("Slug %q already exists", cleanSlug)),
				url.QueryEscape(rawSlug), url.QueryEscape(rawTarget))
			http.Redirect(w, r, errURL, http.StatusSeeOther)
			return
		}
		slog.Error("Failed to save link to database", "slug", cleanSlug, "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to save link"), http.StatusSeeOther)
		return
	}

	slog.Info("Link created successfully", "slug", cleanSlug, "target", cleanTarget, "user", userEmail)
	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Link /%s created", cleanSlug)), http.StatusSeeOther)
}

func (h *AdminHandler) HandleEditLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userEmail, _ := r.Context().Value(auth.UserEmailContextKey).(string)

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		slog.Warn("Failed to parse link edit form", "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	cleanSlug, err := link.ValidateSlug(r.FormValue("slug"))
	if err != nil {
		slog.Warn("Link edit rejected: invalid slug", "raw_slug", r.FormValue("slug"), "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Invalid slug"), http.StatusSeeOther)
		return
	}

	cleanTarget, err := link.ValidateTargetURL(r.FormValue("target"))
	if err != nil {
		slog.Warn("Link edit rejected: invalid target URL", "raw_target", r.FormValue("target"), "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	err = h.store.Update(r.Context(), &link.Link{
		Slug:   cleanSlug,
		Target: cleanTarget,
	})
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			slog.Warn("Link edit target not found", "slug", cleanSlug, "user", userEmail)
			http.Redirect(w, r, "/admin?error="+url.QueryEscape("Link not found"), http.StatusSeeOther)
			return
		}
		slog.Error("Failed to update link in database", "slug", cleanSlug, "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to update link"), http.StatusSeeOther)
		return
	}

	slog.Info("Link updated successfully", "slug", cleanSlug, "new_target", cleanTarget, "user", userEmail)
	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Target for /%s updated", cleanSlug)), http.StatusSeeOther)
}

func (h *AdminHandler) HandleDeleteLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userEmail, _ := r.Context().Value(auth.UserEmailContextKey).(string)

	r.Body = http.MaxBytesReader(w, r.Body, 1024*64)
	if err := r.ParseForm(); err != nil {
		slog.Warn("Failed to parse link delete form", "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to parse request form"), http.StatusSeeOther)
		return
	}

	cleanSlug, err := link.ValidateSlug(r.FormValue("slug"))
	if err != nil {
		slog.Warn("Link delete rejected: invalid slug", "raw_slug", r.FormValue("slug"), "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Invalid slug"), http.StatusSeeOther)
		return
	}

	err = h.store.Delete(r.Context(), cleanSlug)
	if err != nil {
		if errors.Is(err, link.ErrNotFound) {
			slog.Warn("Link delete target not found", "slug", cleanSlug, "user", userEmail)
			http.Redirect(w, r, "/admin?error="+url.QueryEscape("Link not found"), http.StatusSeeOther)
			return
		}
		slog.Error("Failed to delete link from database", "slug", cleanSlug, "error", err, "user", userEmail)
		http.Redirect(w, r, "/admin?error="+url.QueryEscape("Failed to delete link"), http.StatusSeeOther)
		return
	}

	slog.Info("Link deleted successfully", "slug", cleanSlug, "user", userEmail)
	http.Redirect(w, r, "/admin?success="+url.QueryEscape(fmt.Sprintf("Link /%s deleted", cleanSlug)), http.StatusSeeOther)
}
