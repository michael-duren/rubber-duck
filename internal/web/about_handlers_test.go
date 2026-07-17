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
		// The CLI page hard-codes the canonical deployment (duckgc.com),
		// which is also the CLI's default target, so no --base is shown.
		{"/cli", []string{"duck auth login", "duck pull intro-to-concurrency/go", "https://duckgc.com", "releases/latest"}},
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
