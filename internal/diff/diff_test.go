package diff

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

// reconstruct rebuilds both sides from a diff — the fundamental correctness
// invariant: deletes+equals must reproduce old, inserts+equals new.
func reconstruct(lines []Line) (old, new string) {
	var o, n []string
	for _, l := range lines {
		switch l.Op {
		case OpEqual:
			o = append(o, l.Text)
			n = append(n, l.Text)
		case OpDelete:
			o = append(o, l.Text)
		case OpInsert:
			n = append(n, l.Text)
		}
	}
	return strings.Join(o, "\n"), strings.Join(n, "\n")
}

func checkRoundTrip(t *testing.T, oldSrc, newSrc string) []Line {
	t.Helper()
	lines := Lines(oldSrc, newSrc)
	o, n := reconstruct(lines)
	if o != strings.TrimSuffix(oldSrc, "\n") {
		t.Errorf("diff does not reconstruct old side:\ngot  %q\nwant %q", o, oldSrc)
	}
	if n != strings.TrimSuffix(newSrc, "\n") {
		t.Errorf("diff does not reconstruct new side:\ngot  %q\nwant %q", n, newSrc)
	}
	// Line numbers must be sequential per side, 1-based.
	oldN, newN := 0, 0
	for _, l := range lines {
		if l.OldN != 0 {
			if l.OldN != oldN+1 {
				t.Errorf("old line numbers skip: %d after %d (%q)", l.OldN, oldN, l.Text)
			}
			oldN = l.OldN
		}
		if l.NewN != 0 {
			if l.NewN != newN+1 {
				t.Errorf("new line numbers skip: %d after %d (%q)", l.NewN, newN, l.Text)
			}
			newN = l.NewN
		}
	}
	return lines
}

// TestLinesRandomized hammers the round-trip invariant with seeded random
// documents. The tiny vocabulary makes duplicate lines the norm, so this
// exercises the paths the golden tests can't systematically reach: the
// no-anchor replace fallback, lines that become unique only inside a
// recursed sub-segment, and empty/identical/one-sided inputs.
func TestLinesRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	vocab := []string{"a", "b", "c", "d", "", "}", "\tx := 1"}
	randomDoc := func() []string {
		doc := make([]string, rng.Intn(60))
		for i := range doc {
			doc[i] = vocab[rng.Intn(len(vocab))]
		}
		return doc
	}
	for i := range 1000 {
		oldDoc := randomDoc()
		var newDoc []string
		if rng.Intn(10) == 0 {
			newDoc = randomDoc() // occasionally unrelated documents
		} else {
			// Usually a mutation of oldDoc: keep, drop, or insert-before.
			for _, l := range oldDoc {
				switch rng.Intn(5) {
				case 0: // drop
				case 1:
					newDoc = append(newDoc, vocab[rng.Intn(len(vocab))], l)
				default:
					newDoc = append(newDoc, l)
				}
			}
		}
		checkRoundTrip(t, strings.Join(oldDoc, "\n"), strings.Join(newDoc, "\n"))
		if t.Failed() {
			t.Fatalf("failed on case %d: old=%q new=%q", i, oldDoc, newDoc)
		}
	}
}

func countOps(lines []Line) (eq, del, ins int) {
	for _, l := range lines {
		switch l.Op {
		case OpEqual:
			eq++
		case OpDelete:
			del++
		case OpInsert:
			ins++
		}
	}
	return
}

func TestLines(t *testing.T) {
	cases := []struct {
		name                  string
		old, new              string
		wantEq, wantDel, wantIns int
	}{
		{"identical", "a\nb\nc\n", "a\nb\nc\n", 3, 0, 0},
		{"empty to content (new-course proposal)", "", "a\nb\n", 0, 0, 2},
		{"content to empty", "a\nb\n", "", 0, 2, 0},
		{"both empty", "", "", 0, 0, 0},
		{"single line change", "a\nb\nc\n", "a\nX\nc\n", 2, 1, 1},
		{"insert in middle", "a\nb\n", "a\nX\nb\n", 2, 0, 1},
		{"delete from middle", "a\nX\nb\n", "a\nb\n", 2, 1, 0},
		{"no trailing newline equals trailing newline", "a\nb", "a\nb\n", 2, 0, 0},
		{"interleaved changes", "a\n1\nb\n2\nc\n", "a\nX\nb\nY\nc\n", 3, 2, 2},
		{"repeated lines change between anchors", "x\n\ny\n\nz\n", "x\n\nY\n\nz\n", 4, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := checkRoundTrip(t, c.old, c.new)
			eq, del, ins := countOps(lines)
			if eq != c.wantEq || del != c.wantDel || ins != c.wantIns {
				t.Errorf("ops = %d equal / %d delete / %d insert, want %d/%d/%d\n%+v",
					eq, del, ins, c.wantEq, c.wantDel, c.wantIns, lines)
			}
		})
	}
}

// TestLinesMovedBlock pins patience behavior on a moved block: the anchors
// keep the larger side of the move equal and report the minimal edit (the
// single displaced line moves), not a wall of churn.
func TestLinesMovedBlock(t *testing.T) {
	old := "top\nmoved-1\nmoved-2\nmid\nbottom\n"
	new := "top\nmid\nmoved-1\nmoved-2\nbottom\n"
	lines := checkRoundTrip(t, old, new)
	eq, del, ins := countOps(lines)
	if del != 1 || ins != 1 || eq != 4 {
		t.Errorf("ops = %d equal / %d delete / %d insert, want 4/1/1\n%+v", eq, del, ins, lines)
	}
}

func TestHunks(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n"
	new := "1\n2\n3\nX\n5\n6\n7\n8\n9\n10\n11\n12\n13\nY\n15\n"

	hunks := Hunks(Lines(old, new), 2)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2 (changes at lines 4 and 14 are far apart)", len(hunks))
	}
	// First hunk: context 2,3 + delete 4 + insert X + context 5,6.
	if first := hunks[0]; len(first) != 6 || first[0].Text != "2" || first[len(first)-1].Text != "6" {
		t.Errorf("first hunk = %+v", first)
	}

	// With a huge context the two changes merge into one hunk.
	if merged := Hunks(Lines(old, new), 10); len(merged) != 1 {
		t.Errorf("hunks with context 10 = %d, want 1", len(merged))
	}

	// No changes → no hunks.
	if h := Hunks(Lines(old, old), 3); len(h) != 0 {
		t.Errorf("identical hunks = %d, want 0", len(h))
	}
}

// TestLargeFile is the performance smoke test: the biggest real course
// document (~16k lines) diffed against an edited copy of itself must not
// take quadratic time or memory.
func TestLargeFile(t *testing.T) {
	src, err := os.ReadFile("../../courses/educational-os-c.md")
	if err != nil {
		t.Skipf("course file not available: %v", err)
	}
	old := string(src)
	// Sprinkle edits: change every 500th line, insert a block in the middle.
	lines := strings.Split(old, "\n")
	for i := 250; i < len(lines); i += 500 {
		lines[i] = lines[i] + " EDITED"
	}
	mid := len(lines) / 2
	edited := append(append(append([]string{}, lines[:mid]...), "new-1", "new-2", "new-3"), lines[mid:]...)
	new := strings.Join(edited, "\n")

	start := time.Now()
	diff := checkRoundTrip(t, old, new)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("diffing 16k lines took %v — patience recursion has degraded", elapsed)
	}
	if _, del, ins := countOps(diff); del == 0 || ins == 0 {
		t.Errorf("expected edits detected, got %d del / %d ins", del, ins)
	}

	hunks := Hunks(diff, 3)
	if len(hunks) == 0 {
		t.Error("no hunks for an edited document")
	}
	total := 0
	for _, h := range hunks {
		total += len(h)
	}
	if total >= len(lines) {
		t.Errorf("hunks cover %d lines of a %d-line file — context trimming failed", total, len(lines))
	}
	_ = fmt.Sprintf("%d", total)
}
