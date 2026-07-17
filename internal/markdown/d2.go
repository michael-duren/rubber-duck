package markdown

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
	"oss.terrastruct.com/util-go/go2"
)

// A ```d2 fenced code block is compiled to an SVG diagram at ingest instead
// of being syntax-highlighted. Rendering happens once (see package doc), so
// the diagram is authored as text — easy for humans and agents to edit in
// the same markdown — but served as a static picture with no client-side JS.
//
// The site themes via a `.dark` class on <html> (not prefers-color-scheme),
// and a single d2 SVG bakes in one theme's colors. So each block is rendered
// twice — a light and a dark SVG — and CSS shows whichever matches the active
// theme (see .d2-diagram in assets/input.css). The dark render carries a salt
// so the two SVGs' internal CSS class names can't collide on the page.
const d2InfoString = "d2"

// diagramPad is the SVG padding (px) around a diagram's bounding box.
const diagramPad = 8

// d2Logger discards d2's internal slog output; without a logger attached to
// the context, d2 dumps debug traces to stderr and spams ingest logs.
var d2Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

// d2Extension wires the d2 fence transform and renderer into a goldmark
// instance. Register it via goldmark.WithExtensions.
type d2Extension struct{}

func (d2Extension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(d2Transformer{}, 100),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(d2Renderer{}, 100),
	))
}

// d2Block is the AST node a ```d2 fence becomes: it holds the raw diagram
// source, compiled to SVG when the node is rendered.
type d2Block struct {
	ast.BaseBlock
	source []byte
}

var kindD2Block = ast.NewNodeKind("D2Block")

func (*d2Block) Kind() ast.NodeKind { return kindD2Block }

func (n *d2Block) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// d2Transformer replaces every ```d2 fenced code block with a d2Block, so the
// syntax-highlighting renderer (which handles KindFencedCodeBlock) never sees
// it and the d2 renderer does. Other fenced blocks are left untouched.
type d2Transformer struct{}

func (d2Transformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()

	// Collect first, mutate after: replacing children mid-walk is unsafe.
	var targets []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if fcb, ok := n.(*ast.FencedCodeBlock); ok &&
			string(fcb.Language(source)) == d2InfoString {
			targets = append(targets, fcb)
		}
		return ast.WalkContinue, nil
	})

	for _, fcb := range targets {
		block := &d2Block{source: fencedText(fcb, source)}
		fcb.Parent().ReplaceChild(fcb.Parent(), fcb, block)
	}
}

// fencedText reconstructs the body of a fenced code block from its line
// segments (goldmark keeps content as offsets into the source, not a string).
func fencedText(n *ast.FencedCodeBlock, source []byte) []byte {
	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	return buf.Bytes()
}

// d2Renderer renders d2Block nodes to inline SVG.
type d2Renderer struct{}

func (d2Renderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindD2Block, renderD2Block)
}

// DiagramError reports a ```d2 fence whose source failed to compile: the
// document's fault, not the renderer's. Callers can errors.AsType for it to
// report the failure as invalid input rather than an internal error.
type DiagramError struct{ Err error }

func (e *DiagramError) Error() string { return "d2 diagram: " + e.Err.Error() }
func (e *DiagramError) Unwrap() error { return e.Err }

func renderD2Block(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block := node.(*d2Block)
	light, dark, err := compileD2(block.source)
	if err != nil {
		// Abort the whole render so ingest surfaces the bad diagram (with
		// d2's own "line:col: message") rather than serving a page with a
		// silently missing figure.
		return ast.WalkStop, &DiagramError{Err: err}
	}
	_, _ = w.WriteString(`<div class="d2-diagram"><div class="d2-light">`)
	_, _ = w.Write(light)
	_, _ = w.WriteString(`</div><div class="d2-dark">`)
	_, _ = w.Write(dark)
	_, _ = w.WriteString(`</div></div>`)
	return ast.WalkContinue, nil
}

// compileD2 lays the diagram out once and renders it in a light and a dark
// theme. Layout (the expensive step) is theme-independent, so the graph is
// compiled once and only the colorizing Render runs per theme.
func compileD2(src []byte) (light, dark []byte, err error) {
	ctx := d2log.With(context.Background(), d2Logger)

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, nil, fmt.Errorf("text ruler: %w", err)
	}
	layoutResolver := func(string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}

	lightOpts := &d2svg.RenderOpts{
		Pad:         go2.Pointer(int64(diagramPad)),
		ThemeID:     go2.Pointer(d2themescatalog.NeutralDefault.ID),
		NoXMLTag:    go2.Pointer(true), // inline in an HTML page, not a standalone file
		OmitVersion: go2.Pointer(true), // keep output stable across d2 upgrades
	}
	diagram, _, err := d2lib.Compile(ctx, string(src), &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}, lightOpts)
	if err != nil {
		return nil, nil, err
	}

	if light, err = d2svg.Render(diagram, lightOpts); err != nil {
		return nil, nil, err
	}

	darkOpts := *lightOpts
	darkOpts.ThemeID = go2.Pointer(d2themescatalog.DarkMauve.ID)
	darkOpts.Salt = go2.Pointer("dark") // distinct CSS class prefix from the light SVG
	if dark, err = d2svg.Render(diagram, &darkOpts); err != nil {
		return nil, nil, err
	}
	return sizeSVG(light), sizeSVG(dark), nil
}

// d2RootViewBox matches the root <svg>'s viewBox (always "0 0 W H"; the inner
// svg's viewBox starts with a negative pad, so ^ keeps us on the root).
var d2RootViewBox = regexp.MustCompile(`^(<svg\b[^>]*?)viewBox="0 0 (\d+) (\d+)"`)

// sizeSVG gives d2's root <svg> an intrinsic pixel size. d2 emits only a
// viewBox on the root element; without width/height the browser stretches it
// to the full container width and derives an enormous height from the aspect
// ratio. Copying the viewBox dimensions onto width/height makes it a normal
// replaced element that CSS max-width can then scale down (see assets/input.css).
func sizeSVG(svg []byte) []byte {
	return d2RootViewBox.ReplaceAll(svg, []byte(`${1}width="$2" height="$3" viewBox="0 0 $2 $3"`))
}
