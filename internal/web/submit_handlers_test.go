package web

import (
	"net/http/httptest"
	"testing"
)

// TestBackLink covers the Referer-derived "back to the challenge" link: only
// a same-host path survives; anything off-site or oddly-schemed yields "" so
// the page renders no link at all.
func TestBackLink(t *testing.T) {
	cases := []struct {
		name, referer, want string
	}{
		{"same host path", "http://example.com/courses/x/go/lessons/l1", "/courses/x/go/lessons/l1"},
		{"same host with query", "http://example.com/courses?lang=go", "/courses?lang=go"},
		{"relative path", "/courses/x/go", "/courses/x/go"},
		{"no referer", "", ""},
		{"other host", "http://evil.example.net/courses", ""},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"schemeless other host", "//evil.example.net/x", ""},
		// A same-host referer whose *path* starts with "//" must not survive:
		// href="//evil.com/x" is protocol-relative, i.e. off-site.
		{"same host double-slash path", "http://example.com//evil.example.net/x", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://example.com/submissions/1", nil)
			if c.referer != "" {
				r.Header.Set("Referer", c.referer)
			}
			if got := backLink(r); got != c.want {
				t.Errorf("backLink(%q) = %q, want %q", c.referer, got, c.want)
			}
		})
	}
}
