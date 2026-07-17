// Package diff computes line diffs for the proposal review UI. It
// implements patience diff — anchor on lines that are unique to both sides,
// recurse between anchors — rather than a classic LCS dynamic program,
// because course documents run to ~16k lines and an O(n·m) table at that
// size is hundreds of millions of cells. Patience is near-linear on real
// edits and degrades to a plain replace block, never to quadratic work.
package diff

import "strings"

type Op int

const (
	OpEqual Op = iota
	OpDelete
	OpInsert
)

// Line is one row of a diff. OldN/NewN are 1-based line numbers on the side
// the row exists on; 0 means the row has no number there (an insert has no
// old number, a delete no new number).
type Line struct {
	Op   Op
	Text string
	OldN int
	NewN int
}

// Lines diffs two documents line-wise. Deletes are emitted before inserts
// within a changed region. A trailing newline does not produce a final
// empty line, so "a\n" and "a" diff as equal-then-nothing rather than a
// phantom edit.
func Lines(oldSrc, newSrc string) []Line {
	oldLines, newLines := splitLines(oldSrc), splitLines(newSrc)
	var out []Line
	oldN, newN := 1, 1

	emit := func(op Op, lines []string) {
		for _, text := range lines {
			l := Line{Op: op, Text: text}
			switch op {
			case OpEqual:
				l.OldN, l.NewN = oldN, newN
				oldN++
				newN++
			case OpDelete:
				l.OldN = oldN
				oldN++
			case OpInsert:
				l.NewN = newN
				newN++
			}
			out = append(out, l)
		}
	}

	var recurse func(old, new []string)
	recurse = func(old, new []string) {
		// Common prefix.
		p := 0
		for p < len(old) && p < len(new) && old[p] == new[p] {
			p++
		}
		emit(OpEqual, old[:p])
		old, new = old[p:], new[p:]

		// Common suffix (of what remains).
		s := 0
		for s < len(old) && s < len(new) && old[len(old)-1-s] == new[len(new)-1-s] {
			s++
		}
		oldMid, newMid := old[:len(old)-s], new[:len(new)-s]
		suffix := old[len(old)-s:]

		anchors := patienceAnchors(oldMid, newMid)
		if len(anchors) == 0 {
			emit(OpDelete, oldMid)
			emit(OpInsert, newMid)
		} else {
			oi, ni := 0, 0
			for _, a := range anchors {
				recurse(oldMid[oi:a.old], newMid[ni:a.new])
				emit(OpEqual, oldMid[a.old:a.old+1])
				oi, ni = a.old+1, a.new+1
			}
			recurse(oldMid[oi:], newMid[ni:])
		}

		emit(OpEqual, suffix)
	}
	recurse(oldLines, newLines)
	return out
}

type anchor struct{ old, new int }

// patienceAnchors returns, in order, the positions of the longest increasing
// chain of lines that appear exactly once in both slices — the "patience"
// anchors the diff recurses between.
func patienceAnchors(old, new []string) []anchor {
	type count struct{ o, n, oi, ni int }
	counts := make(map[string]*count, len(old)+len(new))
	for i, l := range old {
		c := counts[l]
		if c == nil {
			c = &count{}
			counts[l] = c
		}
		c.o++
		c.oi = i
	}
	for i, l := range new {
		c := counts[l]
		if c == nil {
			c = &count{}
			counts[l] = c
		}
		c.n++
		c.ni = i
	}

	// Candidate pairs in old order; their new positions must form an
	// increasing subsequence to be usable as anchors.
	var pairs []anchor
	for _, l := range old {
		if c := counts[l]; c.o == 1 && c.n == 1 {
			pairs = append(pairs, anchor{c.oi, c.ni})
		}
	}
	if len(pairs) == 0 {
		return nil
	}

	// Longest increasing subsequence on .new (patience sorting itself),
	// O(k log k) in the number of unique-common lines.
	piles := make([]int, 0, len(pairs))       // top value (new index) of each pile
	pileTopPair := make([]int, 0, len(pairs)) // pair index currently topping each pile
	backlink := make([]int, len(pairs))       // previous pair index in the chain
	for i, pr := range pairs {
		lo, hi := 0, len(piles)
		for lo < hi {
			mid := (lo + hi) / 2
			if piles[mid] < pr.new {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == len(piles) {
			piles = append(piles, pr.new)
			pileTopPair = append(pileTopPair, i)
		} else {
			piles[lo] = pr.new
			pileTopPair[lo] = i
		}
		if lo > 0 {
			backlink[i] = pileTopPair[lo-1]
		} else {
			backlink[i] = -1
		}
	}

	chain := make([]anchor, len(piles))
	at := pileTopPair[len(piles)-1]
	for i := len(piles) - 1; i >= 0; i-- {
		chain[i] = pairs[at]
		at = backlink[at]
	}
	return chain
}

// Hunks groups a diff into unified-diff-style hunks: runs of changes plus
// up to context equal lines on each side, with untouched stretches between
// hunks dropped. Changes closer than 2×context merge into one hunk. A diff
// with no changes yields no hunks.
func Hunks(lines []Line, context int) [][]Line {
	// Keep every line within context distance of a change, in either
	// direction; consecutive kept lines form a hunk.
	keep := make([]bool, len(lines))
	dist := context + 1
	for i, l := range lines {
		if l.Op != OpEqual {
			dist = 0
		} else {
			dist++
		}
		keep[i] = dist <= context
	}
	dist = context + 1
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Op != OpEqual {
			dist = 0
		} else {
			dist++
		}
		keep[i] = keep[i] || dist <= context
	}

	var hunks [][]Line
	var cur []Line
	for i, l := range lines {
		switch {
		case keep[i]:
			cur = append(cur, l)
		case cur != nil:
			hunks = append(hunks, cur)
			cur = nil
		}
	}
	if cur != nil {
		hunks = append(hunks, cur)
	}
	return hunks
}

// splitLines splits on '\n' without a phantom empty line for a trailing
// newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
