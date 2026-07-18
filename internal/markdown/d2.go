package markdown

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
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
	"oss.terrastruct.com/d2/d2target"
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

// maxStepFrames caps how many frames a stepped diagram may have. The stepper
// is pure CSS — assets/input.css carries one nth-of-type(:checked) rule per
// frame index — so this constant and the rule count there must match.
const maxStepFrames = 12

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
	frames, err := compileD2(block.source)
	if err != nil {
		// Abort the whole render so ingest surfaces the bad diagram (with
		// d2's own "line:col: message") rather than serving a page with a
		// silently missing figure.
		return ast.WalkStop, &DiagramError{Err: err}
	}
	if len(frames) == 1 {
		writeDiagram(w, frames[0])
		return ast.WalkContinue, nil
	}
	writeStepper(w, block.source, frames)
	return ast.WalkContinue, nil
}

// writeDiagram emits the light/dark SVG pair for one board. CSS in
// assets/input.css shows whichever half matches the active theme.
func writeDiagram(w util.BufWriter, f d2Frame) {
	_, _ = w.WriteString(`<div class="d2-diagram"><div class="d2-light">`)
	_, _ = w.Write(f.light)
	_, _ = w.WriteString(`</div><div class="d2-dark">`)
	_, _ = w.Write(f.dark)
	_, _ = w.WriteString(`</div></div>`)
}

// writeStepper emits a click-through viewer for a multi-frame (D2 `steps:`)
// diagram. The stepping is pure CSS (see .d2-steps in assets/input.css) — no
// client JS, consistent with the rest of the render pipeline.
//
// By default no radio is checked: in that state a CSS keyframe cycle
// (d2cycle-N, offset per frame via the --d2i/--d2n custom properties emitted
// here) auto-plays the frames. Checking any radio — Back, Next, Replay and
// Pause are all <label>s — freezes the cycle and shows that one frame. The
// wrapper is a <form> solely so the Play control can be <button type=reset>:
// resetting unchecks every radio, which resumes autoplay from the first
// frame. The radio group name is derived from the diagram source so output
// is deterministic across ingests.
func writeStepper(w util.BufWriter, source []byte, frames []d2Frame) {
	sum := sha256.Sum256(source)
	id := "d2s-" + hex.EncodeToString(sum[:5])
	n := len(frames)

	_, _ = fmt.Fprintf(w, `<form class="d2-steps" style="--d2n:%d;--d2cyc:d2cycle-%d">`, n, n)
	for i := range frames {
		_, _ = fmt.Fprintf(w, `<input type="radio" name="%s" id="%s-%d">`, id, id, i)
	}
	_, _ = w.WriteString(`<div class="d2-steps-frames">`)
	for i, f := range frames {
		_, _ = fmt.Fprintf(w, `<div class="d2-steps-frame" style="--d2i:%d">`, i)
		writeDiagram(w, f)
		_, _ = w.WriteString(`<div class="d2-steps-nav"><span class="d2-steps-ctl">`)
		// Pause targets this frame's own radio: whichever frame is visible
		// when it's clicked is the frame the stepper freezes on.
		_, _ = fmt.Fprintf(w, `<label class="d2-steps-btn d2-steps-toggle d2-steps-pause" for="%s-%d" title="Pause">&#10074;&#10074;</label>`, id, i)
		_, _ = w.WriteString(`<button type="reset" class="d2-steps-btn d2-steps-toggle d2-steps-play" title="Play from start">&#9654;&#xFE0E;</button>`)
		if i > 0 {
			_, _ = fmt.Fprintf(w, `<label class="d2-steps-btn" for="%s-%d">&#8249; Back</label>`, id, i-1)
		} else {
			_, _ = w.WriteString(`<span class="d2-steps-btn d2-steps-btn-off">&#8249; Back</span>`)
		}
		_, _ = w.WriteString(`</span>`)
		_, _ = fmt.Fprintf(w, `<span class="d2-steps-count">%d&#8202;/&#8202;%d`, i+1, n)
		if f.name != "" {
			_, _ = fmt.Fprintf(w, ` &middot; <span class="d2-steps-name">%s</span>`, html.EscapeString(f.name))
		}
		_, _ = w.WriteString(`</span>`)
		if i < n-1 {
			_, _ = fmt.Fprintf(w, `<label class="d2-steps-btn" for="%s-%d">Next &#8250;</label>`, id, i+1)
		} else {
			_, _ = fmt.Fprintf(w, `<label class="d2-steps-btn" for="%s-0">&#8634;&#xFE0E; Replay</label>`, id)
		}
		_, _ = w.WriteString(`</div></div>`)
	}
	_, _ = w.WriteString(`</div></form>`)
}

// d2Frame is one rendered board of a diagram: the same picture in the light
// and dark theme, plus the board's name (the step key, used as a caption).
type d2Frame struct {
	name  string
	light []byte
	dark  []byte
}

// compileD2 compiles the diagram and renders every board in a light and a
// dark theme. Layout (the expensive step) is theme-independent and happens
// once in Compile; only the colorizing Render runs per theme. A plain diagram
// yields one frame; a D2 `steps:` composition yields one frame per step (plus
// the root board as the starting frame, when it has content of its own).
func compileD2(src []byte) ([]d2Frame, error) {
	ctx := d2log.With(context.Background(), d2Logger)

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("text ruler: %w", err)
	}
	layoutResolver := func(string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}

	baseOpts := &d2svg.RenderOpts{
		Pad:         go2.Pointer(int64(diagramPad)),
		ThemeID:     go2.Pointer(d2themescatalog.NeutralDefault.ID),
		NoXMLTag:    go2.Pointer(true), // inline in an HTML page, not a standalone file
		OmitVersion: go2.Pointer(true), // keep output stable across d2 upgrades
	}
	diagram, _, err := d2lib.Compile(ctx, string(src), &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}, baseOpts)
	if err != nil {
		return nil, err
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 {
		return nil, fmt.Errorf("layers/scenarios are not supported in lessons; use steps")
	}

	var boards []*d2target.Diagram
	if !diagram.IsFolderOnly {
		// The root board's own content is the starting frame; its name stays
		// empty so the stepper shows no caption for it.
		boards = append(boards, diagram)
	}
	for _, s := range diagram.Steps {
		if len(s.Steps) > 0 || len(s.Layers) > 0 || len(s.Scenarios) > 0 {
			return nil, fmt.Errorf("step %q: nested boards inside a step are not supported", s.Name)
		}
		boards = append(boards, s)
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("diagram has no content")
	}
	if len(boards) > maxStepFrames {
		return nil, fmt.Errorf("diagram has %d frames; the stepper supports at most %d", len(boards), maxStepFrames)
	}

	frames := make([]d2Frame, 0, len(boards))
	for i, b := range boards {
		lightOpts := *baseOpts
		darkOpts := *baseOpts
		darkOpts.ThemeID = go2.Pointer(d2themescatalog.DarkMauve.ID)
		if len(boards) == 1 {
			// Preserve the pre-steps output byte for byte: light unsalted,
			// dark salted so the two SVGs' CSS class prefixes differ.
			darkOpts.Salt = go2.Pointer("dark")
		} else {
			// Per-frame salts: near-identical step boards could otherwise
			// hash to the same CSS class prefix with different styles.
			lightOpts.Salt = go2.Pointer(fmt.Sprintf("s%d", i))
			darkOpts.Salt = go2.Pointer(fmt.Sprintf("s%ddark", i))
		}
		light, err := d2svg.Render(b, &lightOpts)
		if err != nil {
			return nil, err
		}
		dark, err := d2svg.Render(b, &darkOpts)
		if err != nil {
			return nil, err
		}
		name := b.Name
		if i == 0 && !diagram.IsFolderOnly {
			name = ""
		}
		frames = append(frames, d2Frame{name: name, light: sizeSVG(light), dark: sizeSVG(dark)})
	}
	return frames, nil
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
