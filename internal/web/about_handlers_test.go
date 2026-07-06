package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocPages(t *testing.T) {
	mux, _ := testMux(t)

	tests := []struct {
		path string
		want []string // substrings that must appear in the body
	}{
		{"/about", []string{"About Rubber Duck", "/cli"}},
		// The CLI page embeds the request origin in its command examples
		// so they're copy-pasteable against this deployment.
		{"/cli", []string{"duck login --base http://example.com", "duck pull", "releases/latest"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s body missing %q", tt.path, want)
				}
			}
		})
	}
}

func TestCLIPageForwardedProto(t *testing.T) {
	mux, _ := testMux(t)
	req := httptest.NewRequest("GET", "/cli", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cli = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "duck login --base https://example.com") {
		t.Error("CLI page should use https origin behind a proxy")
	}
}
