package link

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantErr   bool
		errTarget error
	}{
		{name: "valid simple", input: "foo", want: "foo"},
		{name: "valid uppercase normalized", input: "FOO-bar_123", want: "foo-bar_123"},
		{name: "valid single char", input: "a", want: "a"},
		{name: "valid single digit", input: "7", want: "7"},
		{name: "valid max length 64", input: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
		{name: "empty string", input: "", wantErr: true, errTarget: ErrInvalidInput},
		{name: "only whitespace", input: "   ", wantErr: true, errTarget: ErrInvalidInput},
		{name: "too long 65", input: strings.Repeat("a", 65), wantErr: true, errTarget: ErrInvalidInput},
		{name: "reserved admin", input: "admin", wantErr: true, errTarget: ErrInvalidInput},
		{name: "reserved healthz", input: "healthz", wantErr: true, errTarget: ErrInvalidInput},
		{name: "reserved callback", input: "CALLBACK", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid leading dash", input: "-foo", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid trailing dash", input: "foo-", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid leading underscore", input: "_foo", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid characters slash", input: "foo/bar", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid characters space", input: "foo bar", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid characters unicode", input: "foö", wantErr: true, errTarget: ErrInvalidInput},
		{name: "invalid characters query", input: "foo?bar=1", wantErr: true, errTarget: ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateSlug(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				if tt.errTarget != nil && !errors.Is(err, tt.errTarget) {
					t.Errorf("expected error wrapping %v, got %v", tt.errTarget, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateTargetURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantErr   bool
		errTarget error
	}{
		{name: "valid https", input: "https://example.com/some/path", want: "https://example.com/some/path"},
		{name: "valid http with query", input: "http://example.com:8080/search?q=hello#frag", want: "http://example.com:8080/search?q=hello#frag"},
		{name: "valid uppercase scheme normalized", input: "HTTPS://Example.com/Path", want: "https://Example.com/Path"},
		{name: "empty string", input: "", wantErr: true, errTarget: ErrInvalidInput},
		{name: "whitespace", input: "   ", wantErr: true, errTarget: ErrInvalidInput},
		{name: "javascript scheme rejected", input: "javascript:alert(1)", wantErr: true, errTarget: ErrInvalidInput},
		{name: "data scheme rejected", input: "data:text/html,test", wantErr: true, errTarget: ErrInvalidInput},
		{name: "file scheme rejected", input: "file:///etc/passwd", wantErr: true, errTarget: ErrInvalidInput},
		{name: "relative path rejected", input: "/foo/bar", wantErr: true, errTarget: ErrInvalidInput},
		{name: "no host rejected", input: "http://", wantErr: true, errTarget: ErrInvalidInput},
		{name: "too long rejected", input: "https://example.com/" + strings.Repeat("a", 2050), wantErr: true, errTarget: ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateTargetURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				if tt.errTarget != nil && !errors.Is(err, tt.errTarget) {
					t.Errorf("expected error wrapping %v, got %v", tt.errTarget, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func FuzzValidateSlug(f *testing.F) {
	seeds := []string{
		"a", "1", "foo", "foo-bar", "foo_bar", "ADMIN", "healthz",
		"-bad", "bad-", "", "   ", "a/b", "a?b", "emoji🎉",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		slug, err := ValidateSlug(s)
		if err == nil {
			if len(slug) == 0 || len(slug) > 64 {
				t.Fatalf("valid slug has invalid length: %d", len(slug))
			}
			if slug != strings.ToLower(slug) {
				t.Fatalf("valid slug is not lowercase: %q", slug)
			}
			if _, reserved := reservedSlugs[slug]; reserved {
				t.Fatalf("reserved slug %q was accepted", slug)
			}
			if strings.HasPrefix(slug, "-") || strings.HasPrefix(slug, "_") ||
				strings.HasSuffix(slug, "-") || strings.HasSuffix(slug, "_") {
				t.Fatalf("slug %q has leading/trailing delimiter", slug)
			}
		}
	})
}

func FuzzValidateTargetURL(f *testing.F) {
	seeds := []string{
		"http://example.com",
		"https://example.com/foo?bar=baz",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"ftp://example.com",
		"",
		"https://",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		u, err := ValidateTargetURL(s)
		if err == nil {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				t.Fatalf("accepted target url %q does not start with http:// or https://", u)
			}
			if len(u) > 2048 {
				t.Fatalf("target url exceeds max length: %d", len(u))
			}
		}
	})
}
