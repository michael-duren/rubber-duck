package markdown

import (
	"strings"
	"testing"
)

// TestToHTML_D2Block renders a valid ```d2 fence to the light+dark SVG pair.
func TestToHTML_D2Block(t *testing.T) {
	src := "Before.\n\n```d2\nx -> y -> z\n```\n\nAfter.\n"
	got, err := ToHTML([]byte(src))
	if err != nil {
		t.Fatalf("ToHTML returned error: %v", err)
	}

	for _, want := range []string{
		`<div class="d2-diagram">`,
		`<div class="d2-light">`,
		`<div class="d2-dark">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s", want, got)
		}
	}
	// Two SVGs, one per theme.
	if n := strings.Count(got, "<svg"); n < 2 {
		t.Errorf("expected at least 2 <svg> (light+dark), got %d", n)
	}
	// The diagram is a picture, not a highlighted code block: the literal
	// arrow source must not survive as text.
	if strings.Contains(got, "x -&gt; y -&gt; z") {
		t.Error("d2 source leaked as escaped text; block was not rendered to SVG")
	}
	// Surrounding prose still renders normally.
	if !strings.Contains(got, "<p>Before.</p>") || !strings.Contains(got, "<p>After.</p>") {
		t.Errorf("surrounding prose not rendered:\n%s", got)
	}
}

// TestToHTML_D2Invalid fails the whole render so ingest surfaces the bad
// diagram rather than serving a page with a silently missing figure.
func TestToHTML_D2Invalid(t *testing.T) {
	src := "```d2\nx -> \n```\n"
	if _, err := ToHTML([]byte(src)); err == nil {
		t.Fatal("expected error for malformed d2, got nil")
	}
}

// TestToHTML_NonD2FenceUntouched confirms other fenced blocks keep the
// syntax-highlighting path and are not intercepted by the d2 renderer.
func TestToHTML_NonD2FenceUntouched(t *testing.T) {
	src := "```go\nfunc main() {}\n```\n"
	got, err := ToHTML([]byte(src))
	if err != nil {
		t.Fatalf("ToHTML returned error: %v", err)
	}
	if strings.Contains(got, "d2-diagram") {
		t.Errorf("non-d2 fence was rendered as a diagram:\n%s", got)
	}
	// chroma emits a <pre> with the highlighted source.
	if !strings.Contains(got, "<pre") || !strings.Contains(got, "func") {
		t.Errorf("go fence lost its highlighted code block:\n%s", got)
	}
}
