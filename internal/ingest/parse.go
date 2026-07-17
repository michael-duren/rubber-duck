package ingest

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
)

var docParser = goldmark.New(
	goldmark.WithExtensions(&frontmatter.Extender{}),
	goldmark.WithParserOptions(parser.WithHeadingAttribute()),
)

// Parse validates and structures one course-variant document. On invalid
// input it returns a *ValidationError listing every problem found.
func Parse(src []byte) (*Result, error) {
	pctx := parser.NewContext()
	doc := docParser.Parser().Parse(text.NewReader(src), parser.WithContext(pctx))

	res := &Result{}
	var probs []Problem

	fm := frontmatter.Get(pctx)
	if fm == nil {
		probs = append(probs, Problem{1, "missing YAML frontmatter"})
	} else if err := fm.Decode(&res.Course); err != nil {
		probs = append(probs, Problem{1, "invalid frontmatter: " + err.Error()})
	}
	probs = append(probs, checkFrontmatter(res.Course)...)

	p := &docWalker{src: src, res: res}
	p.walk(doc)
	probs = append(probs, p.probs...)
	probs = append(probs, validate(res)...)

	if len(probs) > 0 {
		return nil, &ValidationError{Problems: probs}
	}
	return res, nil
}

// docWalker is a state machine over the document's top-level blocks.
type docWalker struct {
	src   []byte
	res   *Result
	probs []Problem

	lesson    *ParsedLesson    // current lesson, nil outside one
	challenge *ParsedChallenge // current challenge (incl. final), nil outside one
	isFinal   bool
	fenceDst  *string // where the next fenced code block goes (starter/tests)

	contentStart int     // byte offset where the pending markdown section began
	contentDst   *string // section destination: lesson content or challenge prompt
}

func (p *docWalker) walk(doc ast.Node) {
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch node := n.(type) {
		case *ast.Heading:
			if p.heading(node) {
				continue
			}
		case *ast.FencedCodeBlock:
			if p.fenceDst != nil {
				*p.fenceDst = fenceText(node, p.src)
				p.fenceDst = nil
				continue
			}
		}
	}
	p.flushSection(len(p.src))
	p.closeChallenge()
}

// heading handles boundary headings; returns false for ordinary ones
// (which then simply remain part of the current section's markdown).
func (p *docWalker) heading(h *ast.Heading) bool {
	title := nodeText(h, p.src)
	line := lineOf(p.src, headingOffset(h))
	// The pending section ends where this heading's *line* starts, not at
	// the heading text: headingOffset points past the "# " marker, and
	// flushing to it would leak stray marker characters into the section.
	start := lineStart(p.src, headingOffset(h))

	switch {
	case h.Level == 1 && strings.HasPrefix(title, "Lesson:"):
		p.closeChallenge()
		p.flushSection(start)
		p.res.Lessons = append(p.res.Lessons, ParsedLesson{
			Slug:  attrString(h, "id"),
			Title: strings.TrimSpace(strings.TrimPrefix(title, "Lesson:")),
		})
		p.lesson = &p.res.Lessons[len(p.res.Lessons)-1]
		p.startSection(h, &p.lesson.ContentMD)
		if p.lesson.Slug == "" {
			p.probs = append(p.probs, Problem{line, "lesson is missing an {#slug} attribute"})
		}
		return true

	case h.Level == 1 && strings.HasPrefix(title, "Final Challenge:"):
		p.closeChallenge()
		p.flushSection(start)
		p.lesson = nil
		if p.res.Final.Slug != "" || p.isFinal {
			p.probs = append(p.probs, Problem{line, "more than one final challenge"})
		}
		p.isFinal = true
		p.challenge = &p.res.Final
		p.initChallenge(h, "Final Challenge:", line)
		return true

	case h.Level == 2 && strings.HasPrefix(title, "Challenge:"):
		p.closeChallenge()
		p.flushSection(start)
		if p.lesson == nil {
			p.probs = append(p.probs, Problem{line, "challenge outside a lesson"})
			p.challenge = &ParsedChallenge{} // parse it, then discard
		} else {
			p.lesson.Challenges = append(p.lesson.Challenges, ParsedChallenge{})
			p.challenge = &p.lesson.Challenges[len(p.lesson.Challenges)-1]
		}
		p.initChallenge(h, "Challenge:", line)
		return true

	case h.Level == 3 && p.challenge != nil && (title == "Starter" || title == "Tests"):
		p.flushSection(start)
		p.contentDst = nil
		if title == "Starter" {
			p.fenceDst = &p.challenge.StarterCode
		} else {
			p.fenceDst = &p.challenge.TestCode
		}
		return true
	}
	return false
}

func (p *docWalker) initChallenge(h *ast.Heading, prefix string, line int) {
	title := nodeText(h, p.src)
	p.challenge.Slug = attrString(h, "id")
	p.challenge.Title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
	p.challenge.Line = line
	p.challenge.Points = attrInt(h, "points")
	p.startSection(h, &p.challenge.PromptMD)
	if p.challenge.Slug == "" {
		p.probs = append(p.probs, Problem{line, "challenge is missing an {#slug} attribute"})
	}
	// A slug shaped like a pull ordering prefix would strip back to the
	// wrong slug on the learner's machine. The final challenge is exempt
	// from the "final-" shape only: its directory gets another "final-"
	// prepended, which still strips back correctly, and capstones are
	// naturally named that way (final-duckos, final-editor, …).
	if slug := p.challenge.Slug; OrderingPrefixLen(slug) > 0 &&
		(p.challenge != &p.res.Final || !strings.HasPrefix(slug, "final-")) {
		p.probs = append(p.probs, Problem{line, fmt.Sprintf(
			"challenge slug %q is shaped like a `duck pull` ordering prefix (\"final-\", or two-plus digits and a dash) and would not map back to its directory — rename it", slug)})
	}
	if p.challenge.Points <= 0 {
		p.probs = append(p.probs, Problem{line, "challenge needs a positive points=N attribute"})
	}
}

// closeChallenge verifies the challenge we are leaving was complete.
func (p *docWalker) closeChallenge() {
	c := p.challenge
	if c == nil {
		return
	}
	name := c.Slug
	if name == "" {
		name = c.Title
	}
	if p.fenceDst != nil {
		p.probs = append(p.probs, Problem{c.Line, fmt.Sprintf("challenge %q: heading has no fenced code block after it", name)})
	}
	if c.StarterCode == "" {
		p.probs = append(p.probs, Problem{c.Line, fmt.Sprintf("challenge %q: missing '### Starter' block", name)})
	}
	if c.TestCode == "" {
		p.probs = append(p.probs, Problem{c.Line, fmt.Sprintf("challenge %q: missing '### Tests' block", name)})
	}
	p.challenge = nil
	p.fenceDst = nil
}

// startSection begins accumulating markdown after heading h into dst.
func (p *docWalker) startSection(h *ast.Heading, dst *string) {
	p.contentStart = lineEnd(p.src, headingOffset(h))
	p.contentDst = dst
}

// flushSection stores the markdown between the current section start and stop.
func (p *docWalker) flushSection(stop int) {
	if p.contentDst == nil {
		return
	}
	if stop > p.contentStart {
		*p.contentDst = strings.TrimSpace(string(p.src[p.contentStart:stop]))
	}
	p.contentDst = nil
}

func checkFrontmatter(fm Frontmatter) []Problem {
	var probs []Problem
	need := []struct{ v, name string }{
		{fm.Course, "course"}, {fm.Title, "title"}, {fm.Language, "language"}, {fm.Description, "description"},
	}
	for _, f := range need {
		if strings.TrimSpace(f.v) == "" {
			probs = append(probs, Problem{1, "frontmatter is missing required field " + strconv.Quote(f.name)})
		}
	}
	return probs
}

func validate(res *Result) []Problem {
	var probs []Problem
	if len(res.Lessons) == 0 {
		probs = append(probs, Problem{1, "course has no lessons"})
	}
	if res.Final.Slug == "" && res.Final.Title == "" {
		probs = append(probs, Problem{1, "course has no '# Final Challenge:'"})
	}
	seen := map[string]bool{}
	for _, l := range res.Lessons {
		if l.Slug != "" && seen[l.Slug] {
			probs = append(probs, Problem{1, fmt.Sprintf("duplicate slug %q", l.Slug)})
		}
		seen[l.Slug] = true
		for _, c := range l.Challenges {
			if c.Slug != "" && seen[c.Slug] {
				probs = append(probs, Problem{c.Line, fmt.Sprintf("duplicate slug %q", c.Slug)})
			}
			seen[c.Slug] = true
		}
	}
	if res.Final.Slug != "" && seen[res.Final.Slug] {
		probs = append(probs, Problem{res.Final.Line, fmt.Sprintf("duplicate slug %q", res.Final.Slug)})
	}
	return probs
}

// --- small AST/source helpers ---

func nodeText(n ast.Node, src []byte) string {
	var b bytes.Buffer
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := node.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func fenceText(n *ast.FencedCodeBlock, src []byte) string {
	var b bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// headingOffset is the byte offset of the heading's first text segment.
func headingOffset(h *ast.Heading) int {
	if h.Lines().Len() > 0 {
		return h.Lines().At(0).Start
	}
	return 0
}

// lineOf converts a byte offset to a 1-based line number.
func lineOf(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return bytes.Count(src[:off], []byte("\n")) + 1
}

// lineStart returns the offset of the start of the line containing off.
func lineStart(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

// lineEnd returns the offset just past the end of the line containing off.
func lineEnd(src []byte, off int) int {
	i := bytes.IndexByte(src[off:], '\n')
	if i < 0 {
		return len(src)
	}
	return off + i + 1
}

func attrString(n ast.Node, name string) string {
	v, ok := n.Attribute([]byte(name))
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	}
	return ""
}

// attrInt reads a numeric heading attribute. goldmark parses unquoted
// numbers as float64; quoted ones arrive as byte strings.
func attrInt(n ast.Node, name string) int {
	v, ok := n.Attribute([]byte(name))
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case []byte:
		i, _ := strconv.Atoi(string(t))
		return i
	case string:
		i, _ := strconv.Atoi(t)
		return i
	}
	return 0
}
