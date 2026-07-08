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

# Render the two theme SVGs via the real pipeline using a throwaway test.
test_go="$repo/internal/markdown/zz_preview_test.go"
cleanup() { rm -f "$test_go"; }
trap cleanup EXIT

cat > "$test_go" <<'GO'
package markdown

import (
	"os"
	"regexp"
	"testing"
)

// Throwaway: written and removed by preview.sh. Renders D2_PREVIEW_FILE and
// writes the light/dark SVGs to D2_PREVIEW_OUT/{light,dark}.svg.
func TestZZPreview(t *testing.T) {
	src, err := os.ReadFile(os.Getenv("D2_PREVIEW_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToHTML([]byte("```d2\n" + string(src) + "\n```\n"))
	if err != nil {
		t.Fatalf("diagram did not compile: %v", err)
	}
	light := regexp.MustCompile(`(?s)<div class="d2-light">(.*?)</div><div class="d2-dark">`)
	dark := regexp.MustCompile(`(?s)<div class="d2-dark">(.*?)</div></div>`)
	dir := os.Getenv("D2_PREVIEW_OUT")
	os.WriteFile(dir+"/light.svg", []byte(light.FindStringSubmatch(out)[1]), 0644)
	os.WriteFile(dir+"/dark.svg", []byte(dark.FindStringSubmatch(out)[1]), 0644)
}
GO

D2_PREVIEW_FILE="$(realpath "$d2file")" D2_PREVIEW_OUT="$outdir" \
	go test "$repo/internal/markdown/" -run TestZZPreview >/dev/null

# Intrinsic size is on the root <svg> (sizeSVG put it there). Compute the
# display width after applying BOTH caps, preserving aspect ratio.
read -r W H < <(grep -oE 'width="[0-9]+" height="[0-9]+"' "$outdir/light.svg" | head -1 \
	| grep -oE '[0-9]+' | paste -sd' ')
dispW="$(awk -v w="$W" -v h="$H" -v cw="$COL_W" -v mh="$MAX_H" \
	'BEGIN{ s=1; if (cw/w<s) s=cw/w; if (mh/h<s) s=mh/h; printf "%d", w*s }')"

rsvg-convert -w "$dispW" -b "$LIGHT_BG" "$outdir/light.svg" -o "$outdir/light.png"
rsvg-convert -w "$dispW" -b "$DARK_BG"  "$outdir/dark.svg"  -o "$outdir/dark.png"

echo "intrinsic ${W}x${H}  →  displays at ${dispW}px wide"
echo "$outdir/light.png"
echo "$outdir/dark.png"
