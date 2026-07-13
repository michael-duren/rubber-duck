package web

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// TestCourseArtServesEmbedded: a slug with hand-drawn art gets the embedded
// file, not the generated fallback (the fallback always carries the
// "duck fetch" prompt; hand-drawn art never does).
func TestCourseArtServesEmbedded(t *testing.T) {
	if _, err := staticFS.ReadFile("static/img/courses/intro-to-concurrency.svg"); err != nil {
		t.Skip("no hand-drawn art embedded yet")
	}
	mux, _ := testMux(t)
	rec := getPage(mux, "/courses/intro-to-concurrency/card.svg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	if strings.Contains(rec.Body.String(), "duck fetch") {
		t.Error("got generated fallback for a slug with embedded art")
	}
}

func TestCourseArtFallback(t *testing.T) {
	mux, _ := testMux(t)
	rec := getPage(mux, "/courses/agent-published-later/card.svg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "<svg") || !strings.Contains(body, "duck fetch agent-published-later") {
		t.Errorf("fallback SVG malformed: %.120s", body)
	}
}

func TestGeneratedCardSVG(t *testing.T) {
	a1, a2 := generatedCardSVG("course-a"), generatedCardSVG("course-a")
	if !bytes.Equal(a1, a2) {
		t.Error("same slug produced different art; must be deterministic")
	}
	if b := generatedCardSVG("course-b"); bytes.Equal(a1, b) {
		t.Error("different slugs produced identical art")
	}
	// A hostile slug reaches the generator via the URL path; it must be
	// escaped, not injected into the SVG markup.
	if got := string(generatedCardSVG(`x"><script>`)); strings.Contains(got, "<script>") {
		t.Errorf("slug not escaped: %s", got)
	}
}
