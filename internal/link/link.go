package link

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("link not found")
	ErrConflict     = errors.New("link slug already exists")
	ErrInvalidInput = errors.New("invalid link input")
)

type Link struct {
	Slug      string    `json:"slug"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store interface {
	Get(ctx context.Context, slug string) (*Link, error)
	List(ctx context.Context) ([]*Link, error)
	Create(ctx context.Context, link *Link) error
	Update(ctx context.Context, link *Link) error
	Delete(ctx context.Context, slug string) error
}

var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*[a-z0-9]$|^[a-z0-9]$`)

var reservedSlugs = map[string]struct{}{
	"healthz":     {},
	"admin":       {},
	"auth":        {},
	"login":       {},
	"logout":      {},
	"callback":    {},
	"static":      {},
	"favicon.ico": {},
	"robots.txt":  {},
}

func ValidateSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if len(slug) == 0 {
		return "", fmt.Errorf("%w: slug cannot be empty", ErrInvalidInput)
	}
	if len(slug) > 64 {
		return "", fmt.Errorf("%w: slug length exceeds 64 characters", ErrInvalidInput)
	}
	if !slugRegex.MatchString(slug) {
		return "", fmt.Errorf("%w: slug must contain only alphanumeric characters, dashes, and underscores", ErrInvalidInput)
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		return "", fmt.Errorf("%w: slug %q is reserved", ErrInvalidInput, slug)
	}
	return slug, nil
}

func ValidateTargetURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("%w: target URL cannot be empty", ErrInvalidInput)
	}
	if len(trimmed) > 2048 {
		return "", fmt.Errorf("%w: target URL exceeds maximum length of 2048 characters", ErrInvalidInput)
	}

	u, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: invalid target URL format: %v", ErrInvalidInput, err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https, got %q", ErrInvalidInput, u.Scheme)
	}

	if u.Host == "" {
		return "", fmt.Errorf("%w: target URL must include a host", ErrInvalidInput)
	}

	// Normalize scheme to lowercase
	u.Scheme = scheme
	return u.String(), nil
}
