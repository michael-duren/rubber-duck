package grader

import (
	"strconv"
	"testing"
)

func intp(n int) *int { return &n }

func TestParseTestCounts(t *testing.T) {
	cases := []struct {
		name       string
		language   string
		output     string
		wantPassed *int
		wantTotal  *int
	}{
		{
			name:     "go all pass, no subtests",
			language: "go",
			output: `=== RUN   TestDouble
--- PASS: TestDouble (0.00s)
PASS
ok  	challenge	0.002s
`,
			wantPassed: intp(1), wantTotal: intp(1),
		},
		{
			name:     "go subtests don't double-count the parent line",
			language: "go",
			output: `=== RUN   TestSum
=== RUN   TestSum/basic
--- PASS: TestSum/basic (0.00s)
=== RUN   TestSum/empty
--- PASS: TestSum/empty (0.00s)
=== RUN   TestSum/single
--- FAIL: TestSum/single (0.00s)
--- FAIL: TestSum (0.00s)
FAIL
`,
			wantPassed: intp(2), wantTotal: intp(3),
		},
		{
			name:     "go no test lines at all (panic/timeout) is unparseable",
			language: "go",
			output: `panic: test timed out after 25s
goroutine 5 [running]:
`,
			wantPassed: nil, wantTotal: nil,
		},
		{
			name:       "python all pass",
			language:   "python",
			output:     ".....                                                                    [100%]\n5 passed in 0.02s\n",
			wantPassed: intp(5), wantTotal: intp(5),
		},
		{
			name:       "python mixed pass/fail",
			language:   "python",
			output:     "..F                                                                      [100%]\n2 passed, 1 failed in 0.03s\n",
			wantPassed: intp(2), wantTotal: intp(3),
		},
		{
			name:       "python errors count toward total, not passed",
			language:   "python",
			output:     "1 passed, 1 error in 0.01s\n",
			wantPassed: intp(1), wantTotal: intp(2),
		},
		{
			name:       "python no summary line is unparseable",
			language:   "python",
			output:     "ImportError: cannot import name 'sum_nums'\n",
			wantPassed: nil, wantTotal: nil,
		},
		{
			name:     "c mixed pass/fail",
			language: "c",
			output: `--- PASS: test_sum_basic
--- PASS: test_sum_empty
--- FAIL: test_sum_negative
`,
			wantPassed: intp(2), wantTotal: intp(3),
		},
		{
			name:       "c compile error is unparseable",
			language:   "c",
			output:     "solution.c:3:1: error: expected ';' before '}' token\n",
			wantPassed: nil, wantTotal: nil,
		},
		{
			name:       "unknown language",
			language:   "cobol",
			output:     "5 passed in 0.02s\n",
			wantPassed: nil, wantTotal: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			passed, total := ParseTestCounts(c.language, c.output)
			if !intPtrEqual(passed, c.wantPassed) || !intPtrEqual(total, c.wantTotal) {
				t.Errorf("ParseTestCounts() = (%s, %s), want (%s, %s)",
					intPtrString(passed), intPtrString(total), intPtrString(c.wantPassed), intPtrString(c.wantTotal))
			}
		})
	}
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func intPtrString(p *int) string {
	if p == nil {
		return "nil"
	}
	return strconv.Itoa(*p)
}
