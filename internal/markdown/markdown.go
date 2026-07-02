// Package markdown renders markdown to HTML with server-side syntax
// highlighting. Rendering happens once at ingest; pages serve cached HTML.
package markdown

import (
	"bytes"
	"fmt"

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
