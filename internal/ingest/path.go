package ingest

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/markdown"
)

// PathFrontmatter is a learning-path document's YAML header.
type PathFrontmatter struct {
	Path        string   `yaml:"path"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Courses     []string `yaml:"courses"`
}

// PathResult is a parsed and validated learning-path document.
type PathResult struct {
	Path       PathFrontmatter
	OverviewMD string
}

// IsPathDocument reports whether src looks like a learning-path document
// rather than a course: its frontmatter carries a "path" key. Used by
// callers (the seed command) that accept either kind of file.
func IsPathDocument(src []byte) bool {
	pctx := parser.NewContext()
	docParser.Parser().Parse(text.NewReader(src), parser.WithContext(pctx))
	fm := frontmatter.Get(pctx)
	if fm == nil {
		return false
	}
	var p PathFrontmatter
	if err := fm.Decode(&p); err != nil {
		return false
	}
	return strings.TrimSpace(p.Path) != ""
}

// ParsePath validates and structures one learning-path document — the
// second, much smaller agent contract next to course documents (Parse):
//
//	--- yaml frontmatter: path, title, description, courses required ---
//	free markdown body — the path's long-form overview
//
// The ordered courses list references course slugs; whether each slug
// resolves to a published course is checked at storage time, not here,
// because parsing stays pure (no I/O). On invalid input it returns a
// *ValidationError listing every problem found, in the same shape Parse
// uses for courses.
func ParsePath(src []byte) (*PathResult, error) {
	pctx := parser.NewContext()
	docParser.Parser().Parse(text.NewReader(src), parser.WithContext(pctx))

	res := &PathResult{}
	var probs []Problem

	fm := frontmatter.Get(pctx)
	if fm == nil {
		probs = append(probs, Problem{1, "missing YAML frontmatter"})
	} else if err := fm.Decode(&res.Path); err != nil {
		probs = append(probs, Problem{1, "invalid frontmatter: " + err.Error()})
	}

	need := []struct{ v, name string }{
		{res.Path.Path, "path"}, {res.Path.Title, "title"}, {res.Path.Description, "description"},
	}
	for _, f := range need {
		if strings.TrimSpace(f.v) == "" {
			probs = append(probs, Problem{1, fmt.Sprintf("frontmatter is missing required field %q", f.name)})
		}
	}
	if len(res.Path.Courses) == 0 {
		probs = append(probs, Problem{1, "frontmatter needs a non-empty \"courses\" list (ordered course slugs)"})
	}
	seen := map[string]bool{}
	for _, slug := range res.Path.Courses {
		if strings.TrimSpace(slug) == "" {
			probs = append(probs, Problem{1, "\"courses\" contains an empty slug"})
			continue
		}
		if seen[slug] {
			probs = append(probs, Problem{1, fmt.Sprintf("duplicate course slug %q in \"courses\"", slug)})
		}
		seen[slug] = true
	}

	res.OverviewMD = pathBody(src)

	if len(probs) > 0 {
		return nil, &ValidationError{Problems: probs}
	}
	return res, nil
}

// PathToDomain renders HTML for the description and overview and assembles
// the domain path ready for storage. src is the original document, kept
// verbatim as the path's source of truth.
func PathToDomain(res *PathResult, src []byte) (domain.LearningPath, error) {
	descHTML, err := markdown.ToHTML([]byte(res.Path.Description))
	if err != nil {
		return domain.LearningPath{}, err
	}
	overviewHTML := ""
	if res.OverviewMD != "" {
		if overviewHTML, err = markdown.ToHTML([]byte(res.OverviewMD)); err != nil {
			return domain.LearningPath{}, err
		}
	}
	return domain.LearningPath{
		Slug:            res.Path.Path,
		Title:           res.Path.Title,
		DescriptionMD:   res.Path.Description,
		DescriptionHTML: descHTML,
		OverviewMD:      res.OverviewMD,
		OverviewHTML:    overviewHTML,
		SourceMD:        string(src),
		CourseSlugs:     res.Path.Courses,
	}, nil
}

// pathBody returns the markdown after the leading "---"-delimited YAML
// frontmatter block. goldmark's frontmatter extender only records the
// decoded YAML, not where the block ended, so the delimiter scan is manual.
func pathBody(src []byte) string {
	if !bytes.HasPrefix(src, []byte("---")) {
		return strings.TrimSpace(string(src))
	}
	end := bytes.Index(src[3:], []byte("\n---"))
	if end < 0 {
		return strings.TrimSpace(string(src))
	}
	rest := src[3+end+len("\n---"):]
	if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		rest = nil
	}
	return strings.TrimSpace(string(rest))
}
