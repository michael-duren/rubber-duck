package markdown

import (
	"strings"
	"testing"
)

func TestStripFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "no frontmatter",
			src:  "# Hello\n\nBody text.\n",
			want: "# Hello\n\nBody text.\n",
		},
		{
			name: "yaml frontmatter stripped",
			src:  "---\ncourse: intro\nlanguage: go\n---\n\n# Hello\n\nBody text.\n",
			want: "\n# Hello\n\nBody text.\n",
		},
		{
			name: "longer delimiter",
			src:  "-----\ncourse: intro\n-----\n\nBody.\n",
			want: "\nBody.\n",
		},
		{
			name: "unterminated frontmatter left untouched",
			src:  "---\ncourse: intro\n\nBody.\n",
			want: "---\ncourse: intro\n\nBody.\n",
		},
		{
			name: "thematic break not on line 1 is untouched",
			src:  "# Title\n\n---\n\nBody.\n",
			want: "# Title\n\n---\n\nBody.\n",
		},
		{
			name: "empty input",
			src:  "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(StripFrontmatter([]byte(c.src)))
			if got != c.want {
				t.Errorf("StripFrontmatter(%q) = %q, want %q", c.src, got, c.want)
			}
		})
	}
}

// TestStripFrontmatterThenToHTML is the real usage: the remainder after
// stripping renders as ordinary document body, with none of the frontmatter
// keys leaking into the output.
func TestStripFrontmatterThenToHTML(t *testing.T) {
	src := "---\ncourse: intro-to-go\nlanguage: go\ntitle: Intro\ndescription: A course\n---\n\n" +
		"# Lesson: Getting Started\n\nSome prose.\n\n```go\nfunc main() {}\n```\n"

	html, err := ToHTML(StripFrontmatter([]byte(src)))
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if strings.Contains(html, "course:") || strings.Contains(html, "language:") {
		t.Errorf("rendered HTML should not contain frontmatter keys, got: %s", html)
	}
	if !strings.Contains(html, "<h1") {
		t.Errorf("expected a rendered heading, got: %s", html)
	}
	if !strings.Contains(html, "<pre") && !strings.Contains(html, "<code") {
		t.Errorf("expected a rendered code block, got: %s", html)
	}
}
