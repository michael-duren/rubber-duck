package markdown

import (
	"fmt"
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

// TestToHTML_D2Steps renders a `steps:` composition as a CSS-only stepper:
// one frame per board (root + each step), radio inputs, and Back/Next labels.
func TestToHTML_D2Steps(t *testing.T) {
	src := "```d2\n" +
		"a -> b\n" +
		"steps: {\n" +
		"  \"insert 4\": { a.style.stroke: \"#dc2626\" }\n" +
		"  \"done\": { b.style.stroke: \"#dc2626\" }\n" +
		"}\n" +
		"```\n"
	got, err := ToHTML([]byte(src))
	if err != nil {
		t.Fatalf("ToHTML returned error: %v", err)
	}

	// Root board + 2 steps = 3 frames, each with a radio and a light/dark pair.
	if n := strings.Count(got, `class="d2-steps-frame"`); n != 3 {
		t.Errorf("expected 3 frames, got %d", n)
	}
	if n := strings.Count(got, `type="radio"`); n != 3 {
		t.Errorf("expected 3 radio inputs, got %d", n)
	}
	if n := strings.Count(got, `<div class="d2-diagram">`); n != 3 {
		t.Errorf("expected 3 d2-diagram wrappers, got %d", n)
	}
	// Step names surface as captions; nav and autoplay controls exist. The
	// --d2n/--d2cyc/--d2i custom properties drive the CSS autoplay cycle.
	for _, want := range []string{
		`<form class="d2-steps" style="--d2n:3;--d2cyc:d2cycle-3">`,
		`<div class="d2-steps-frame" style="--d2i:2">`,
		`insert 4`,
		`Next &#8250;</label>`,
		`&#8249; Back</label>`,
		`&#8634;&#xFE0E; Replay</label>`,
		`<button type="reset" class="d2-steps-btn d2-steps-toggle d2-steps-play"`,
		`</div></form>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// No radio starts checked: the unchecked state is what auto-plays.
	if strings.Contains(got, " checked") {
		t.Error("expected no checked radio (autoplay is the default state)")
	}
	// One Pause label per frame, each targeting its own frame's radio.
	if n := strings.Count(got, `title="Pause"`); n != 3 {
		t.Errorf("expected 3 pause labels, got %d", n)
	}
}

// TestToHTML_D2StepsTooMany enforces the frame cap: the pure-CSS stepper has
// a fixed number of :checked rules (see maxStepFrames / assets/input.css).
func TestToHTML_D2StepsTooMany(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("```d2\na -> b\nsteps: {\n")
	for i := range maxStepFrames { // root board + maxStepFrames steps = cap+1
		fmt.Fprintf(&sb, "  s%d: { a.label: \"%d\" }\n", i, i)
	}
	sb.WriteString("}\n```\n")
	if _, err := ToHTML([]byte(sb.String())); err == nil {
		t.Fatal("expected error for too many step frames, got nil")
	}
}

// TestToHTML_D2StaticUnchanged pins the single-board output shape: no stepper
// chrome leaks into plain diagrams.
func TestToHTML_D2StaticUnchanged(t *testing.T) {
	got, err := ToHTML([]byte("```d2\nx -> y\n```\n"))
	if err != nil {
		t.Fatalf("ToHTML returned error: %v", err)
	}
	for _, banned := range []string{"d2-steps", "radio", "Back", "Next"} {
		if strings.Contains(got, banned) {
			t.Errorf("static diagram output contains stepper markup %q", banned)
		}
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
