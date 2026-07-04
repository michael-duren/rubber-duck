package grader

import (
	"regexp"
	"strconv"
	"strings"
)

// ParseTestCounts extracts per-test pass/total counts from a runner's
// captured output, for proportional scoring. It returns nil, nil if the
// language is unknown or the output doesn't match the expected shape.
func ParseTestCounts(language, output string) (passed, total *int) {
	switch language {
	case "go":
		return parseGoTestCounts(output)
	// C tests print one `--- PASS: name` / `--- FAIL: name` line per test
	// case (the documented course contract, mirroring `go test -v`), so the
	// go parser applies as-is.
	case "c":
		return parseGoTestCounts(output)
	case "python":
		return parsePytestCounts(output)
	default:
		return nil, nil
	}
}

var goTestLineRE = regexp.MustCompile(`(?m)^\s*--- (PASS|FAIL): (\S+)`)

// parseGoTestCounts counts individual test cases from `go test -v` output.
// Table-driven subtests (t.Run) each get their own --- PASS/FAIL line, but
// so does the parent test that ran them (an aggregate of its subtests) —
// counting every line would double-count. Only leaf entries (names with no
// further nested "name/..." line) are counted.
func parseGoTestCounts(output string) (passed, total *int) {
	matches := goTestLineRE.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	type entry struct {
		name string
		pass bool
	}
	entries := make([]entry, 0, len(matches))
	names := make(map[string]bool, len(matches))
	for _, m := range matches {
		entries = append(entries, entry{name: m[2], pass: m[1] == "PASS"})
		names[m[2]] = true
	}
	isParent := make(map[string]bool, len(names))
	for name := range names {
		for other := range names {
			if other != name && strings.HasPrefix(other, name+"/") {
				isParent[name] = true
				break
			}
		}
	}

	var p, t int
	for _, e := range entries {
		if isParent[e.name] {
			continue
		}
		t++
		if e.pass {
			p++
		}
	}
	if t == 0 {
		return nil, nil
	}
	return &p, &t
}

// pytest -q ends with a summary line like "3 passed, 2 failed in 0.05s" or
// "1 passed, 1 error in 0.01s"; any subset of the counted words may be
// absent (e.g. just "5 passed in 0.02s" when everything succeeds).
var pytestSummaryRE = regexp.MustCompile(`(\d+) (passed|failed|error)`)

func parsePytestCounts(output string) (passed, total *int) {
	matches := pytestSummaryRE.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	var p, t int
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		t += n
		if m[2] == "passed" {
			p += n
		}
	}
	if t == 0 {
		return nil, nil
	}
	return &p, &t
}
