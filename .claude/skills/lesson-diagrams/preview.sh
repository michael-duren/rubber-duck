#!/usr/bin/env bash
#
# preview.sh — render a D2 lesson diagram exactly as the site renders it, so
# you can READ it before committing. It runs the diagram through the project's
# own markdown.ToHTML (same theme + sizeSVG treatment the site uses), then
# rasterizes the light AND dark SVGs to PNG *at the on-screen display size*
# (after the max-width / max-height caps in assets/input.css), not the
# misleading intrinsic size that a raw d2 render would give you.
#
# Usage:
#   .claude/skills/lesson-diagrams/preview.sh path/to/diagram.d2 [outdir]
#
# The .d2 file is the raw diagram source (no ```d2 fence). Prints the two PNG
# paths; open/Read them to check legibility in both themes.
#
# Requires: go, rsvg-convert (librsvg), awk.
set -euo pipefail

d2file="${1:?usage: preview.sh path/to/diagram.d2 [outdir]}"
outdir="${2:-$(mktemp -d)}"
repo="$(git rev-parse --show-toplevel)"
mkdir -p "$outdir"

# Column width and height cap must match assets/input.css (.d2-light>svg).
COL_W=700
MAX_H=520
LIGHT_BG="#f1f7f2"   # body bg in light theme (layout.templ)
DARK_BG="#0b0f0d"    # body bg in dark theme

# Render the theme SVGs via the real pipeline using a throwaway test. The
# whole write→test→remove dance holds a lock so concurrent preview runs
# (e.g. parallel agents) don't clobber each other's test file.
test_go="$repo/internal/markdown/zz_preview_test.go"
lock="${TMPDIR:-/tmp}/d2-preview-$(id -u).lock"
exec 9>"$lock"
flock 9
cleanup() { rm -f "$test_go"; }
trap cleanup EXIT

cat > "$test_go" <<'GO'
package markdown

import (
	"fmt"
	"os"
	"regexp"
	"testing"
)

// Throwaway: written and removed by preview.sh. Renders D2_PREVIEW_FILE and
// writes each frame's light/dark SVGs to D2_PREVIEW_OUT. A plain diagram is
// one frame (light.svg/dark.svg); a `steps:` diagram writes one numbered
// pair per frame (light-1.svg, dark-1.svg, ...).
func TestZZPreview(t *testing.T) {
	src, err := os.ReadFile(os.Getenv("D2_PREVIEW_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToHTML([]byte("```d2\n" + string(src) + "\n```\n"))
	if err != nil {
		t.Fatalf("diagram did not compile: %v", err)
	}
	pair := regexp.MustCompile(`(?s)<div class="d2-light">(.*?)</div><div class="d2-dark">(.*?)</div></div>`)
	frames := pair.FindAllStringSubmatch(out, -1)
	dir := os.Getenv("D2_PREVIEW_OUT")
	for i, f := range frames {
		suffix := ""
		if len(frames) > 1 {
			suffix = fmt.Sprintf("-%d", i+1)
		}
		os.WriteFile(dir+"/light"+suffix+".svg", []byte(f[1]), 0644)
		os.WriteFile(dir+"/dark"+suffix+".svg", []byte(f[2]), 0644)
	}
}
GO

# -count=1: the .d2 lives outside the module, so go's test cache can't see
# its content change and would happily replay a stale PASS that wrote nothing.
D2_PREVIEW_FILE="$(realpath "$d2file")" D2_PREVIEW_OUT="$outdir" \
	go test "$repo/internal/markdown/" -run TestZZPreview -count=1 >/dev/null

# Intrinsic size is on the root <svg> (sizeSVG put it there). Compute the
# display width after applying BOTH caps, preserving aspect ratio. One PNG
# pair per frame (a `steps:` diagram writes light-1.svg, light-2.svg, ...).
for light in "$outdir"/light*.svg; do
	dark="${light/light/dark}"
	read -r W H < <(grep -oE 'width="[0-9]+" height="[0-9]+"' "$light" | head -1 \
		| grep -oE '[0-9]+' | paste -sd' ')
	dispW="$(awk -v w="$W" -v h="$H" -v cw="$COL_W" -v mh="$MAX_H" \
		'BEGIN{ s=1; if (cw/w<s) s=cw/w; if (mh/h<s) s=mh/h; printf "%d", w*s }')"

	rsvg-convert -w "$dispW" -b "$LIGHT_BG" "$light" -o "${light%.svg}.png"
	rsvg-convert -w "$dispW" -b "$DARK_BG"  "$dark"  -o "${dark%.svg}.png"

	echo "$(basename "$light" .svg): intrinsic ${W}x${H}  →  displays at ${dispW}px wide"
	echo "${light%.svg}.png"
	echo "${dark%.svg}.png"
done
