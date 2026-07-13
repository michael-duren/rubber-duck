package web

import (
	"fmt"
	"hash/fnv"
	"html"
	"net/http"
	"strings"
)

// courseArt serves the course card art at /courses/{slug}/card.svg. Courses
// with hand-drawn art ship it embedded under static/img/courses/; any other
// slug — agents publish new courses through the API all the time — gets a
// deterministic generated pane instead, so every card has a distinct visual
// without a repo change. Unknown slugs are not 404s for that reason.
func (h *handlers) courseArt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	slug := r.PathValue("slug")
	if b, err := staticFS.ReadFile("static/img/courses/" + slug + ".svg"); err == nil {
		w.Write(b)
		return
	}
	w.Write(generatedCardSVG(slug))
}

// ansiAccents mirrors the site's rainbow rule (assets/input.css): each card
// takes one hue from it, so the catalog reads as that gradient spread over a
// grid. Hand-drawn art follows the same palette by convention.
var ansiAccents = []string{
	"#f87171", // red-400
	"#fbbf24", // amber-400
	"#34d399", // emerald-400
	"#22d3ee", // cyan-400
	"#a78bfa", // violet-400
	"#e879f9", // fuchsia-400
}

// generatedCardSVG draws the fallback card: the shared terminal chrome (dark
// pane, prompt line) around a randomart fingerprint of the slug — same idea
// as ssh-keygen's, a texture that is stable per course and visibly different
// between courses. Everything derives from an FNV seed; no global rand.
func generatedCardSVG(slug string) []byte {
	seed := fnv.New64a()
	seed.Write([]byte(slug))
	rng := seed.Sum64()
	next := func() uint64 { // xorshift64: cheap, deterministic, good enough for texture
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17
		return rng
	}
	accent := ansiAccents[next()%uint64(len(ansiAccents))]

	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 256" font-family="ui-monospace,'Cascadia Code',Menlo,Consolas,monospace">`)
	b.WriteString(`<rect width="640" height="256" fill="#0b120e"/>`)
	fmt.Fprintf(&b, `<text xml:space="preserve" x="24" y="36" font-size="15"><tspan fill="#34d399">$</tspan><tspan fill="#cbd5e1"> duck fetch %s</tspan></text>`, html.EscapeString(slug))
	b.WriteString(`<line x1="24" y1="52" x2="616" y2="52" stroke="#1e2b24" stroke-width="1"/>`)

	// 24×6 grid of cells at four densities (blank/faint/mid/bright), the
	// terminal equivalent of ░▒▓ shading.
	opacities := []string{"", "0.14", "0.38", "0.85"}
	for row := range 6 {
		for col := range 24 {
			op := opacities[next()%4]
			if op == "" {
				continue
			}
			x := 24 + col*25
			y := 70 + row*27
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="19" height="21" fill="%s" opacity="%s"/>`, x, y, accent, op)
		}
	}
	b.WriteString(`</svg>`)
	return []byte(b.String())
}
