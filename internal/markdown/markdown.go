// Package markdown renders markdown to HTML with server-side syntax
// highlighting. Rendering happens once at ingest; pages serve cached HTML.
package markdown

import (
	"bytes"
	"fmt"
	"regexp"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github-dark"),
			highlighting.WithFormatOptions(chromahtml.TabWidth(4)),
		),
	),
	goldmark.WithParserOptions(parser.WithHeadingAttribute()),
)

// ToHTML renders a markdown fragment.
func ToHTML(src []byte) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return buf.String(), nil
}

// frontmatterDelim matches a YAML frontmatter delimiter line: three or more
// '-' characters and nothing else but trailing horizontal whitespace. This
// mirrors the delimiter convention internal/ingest parses via
// go.abhg.dev/goldmark/frontmatter (YAML format, Delim: '-', "There must be
// at least three of these in a row").
var frontmatterDelim = regexp.MustCompile(`^-{3,}[ \t]*$`)

// StripFrontmatter removes a leading YAML frontmatter block — a delimiter
// line, the block content, and a matching closing delimiter line — from src,
// so the remainder can be handed to ToHTML as plain document body instead of
// rendering the frontmatter as a stray paragraph/rule. It's a simple line
// scan rather than a real YAML parser: the preview only needs to not render
// the block, not decode its fields (that's ingest.Parse's job for a real
// save). If src has no leading frontmatter delimiter on its first line, or
// no closing delimiter is ever found, src is returned unchanged.
func StripFrontmatter(src []byte) []byte {
	lines := bytes.SplitAfter(src, []byte("\n"))
	if len(lines) == 0 || !frontmatterDelim.Match(bytes.TrimRight(lines[0], "\r\n")) {
		return src
	}
	for i := 1; i < len(lines); i++ {
		if frontmatterDelim.Match(bytes.TrimRight(lines[i], "\r\n")) {
			return bytes.Join(lines[i+1:], nil)
		}
	}
	return src
}
