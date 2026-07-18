---
name: lesson-diagrams
description: >
  Author or edit diagrams in Rubber Duck course lessons (courses/*.md) using
  D2. Use when a lesson would be clearer with a picture of a struct, array,
  linked list / chain, pointer relationship, or a hash / put / get / resize
  flow — or when asked to add, fix, or resize a diagram in a course. Covers
  the render pipeline, the sizing traps, theme-safe styling, and a preview
  helper so you can read the diagram before committing.
---

# Lesson diagrams (D2)

Lessons can embed diagrams as text: a fenced ` ```d2 ` block is compiled to
inline SVG **once at ingest** by `internal/markdown/d2.go` (goldmark
extension). No client-side JS, no image files — the diagram lives in the
markdown, so humans and agents edit it in the same PR as the prose.

Each block is rendered **twice** — a light and a dark SVG — and CSS shows
whichever matches the site's `.dark` theme class. Sizing/scaling rules live in
`assets/input.css` (`.d2-diagram`). Do not paste raw `<svg>`/`<div>` HTML into
a lesson: goldmark runs without `WithUnsafe`, so raw HTML is silently dropped.
Mermaid is **not** supported; D2 is the only diagram tool.

## First decide: D2 or ASCII?

Match the tool to the picture. Not everything should be a D2 diagram.

- **ASCII / Unicode box-art in a plain code fence** — best for *contiguous*
  layouts: array cells, bit patterns, memory offsets, byte tables. Monospace
  cells map 1:1 to memory. This is already the house style (see the FNV bit
  tables in `build-a-hashmap-c.md`). Zero infra, edits cleanly.
- **D2** — best for *graph-shaped* things: struct field relationships, linked
  chains, pointer-to-pointer, and hash/put/get/resize flows. Auto-layout from
  text.

If a monospace grid would say it, use ASCII. If arrows and boxes would say it,
use D2.

## Authoring workflow

1. **Draft** the diagram as a `.d2` file in the scratchpad (raw source, no
   fence) so you can iterate without touching the course.
2. **Preview it at real display size** — this is the step that catches every
   mistake:
   ```
   .claude/skills/lesson-diagrams/preview.sh /path/to/draft.d2
   ```
   It compiles the diagram through the real pipeline (fails loudly with d2's
   `line:col: message` if the syntax is wrong) and rasterizes the **light and
   dark** SVGs at the **on-screen size after the CSS caps**. Read both PNGs.
   Do NOT judge size from a raw `d2`/rsvg render — that shows the *intrinsic*
   size; the browser caps it (see Sizing) and small text only shows up at
   display size.
3. **Tune** until it reads well in both themes (Sizing + Theming below).
4. **Insert** the ` ```d2 ` block into the lesson with a one-line caption
   above it that names any visual convention you used (e.g. "the dashed arrow
   is where the pointer points *after* the unlink").
5. **Validate the whole course** parses/renders end to end:
   ```
   go run ./cmd/coursecheck courses/<file>.md      # expect: INGEST OK
   ```
6. **Re-seed to view in the running app.** Editing the `.md` does nothing to
   the DB by itself — the site serves HTML rendered at ingest time, and
   `make dev` only auto-seeds an *empty* database. With the dev server up:
   ```
   go run ./cmd/duckserver seed courses/<file>.md
   ```

## Sizing — the thing that goes wrong

The browser caps every diagram (`assets/input.css`): **`max-width: 100%`** (the
lesson prose column, ~700px) and **`max-height: 520px`**. The diagram is scaled
to fit inside that box, preserving aspect ratio. Consequences:

- **Wide-and-short** (e.g. a long horizontal chain, 1300×100) scales down to
  column width → the text becomes tiny.
- **Tall-and-narrow** (e.g. a 5-step vertical flow, 400×1500) hits the height
  cap → also tiny, and it towers over the page.
- **More elements = smaller text**, because the whole thing shrinks to fit.

Aim for a balanced aspect and a **modest element count** so it renders large:
roughly **intrinsic width ≤ ~900px and height ≤ ~520px**. `preview.sh` prints
the intrinsic size and the resulting display width — if display width is much
below 700 or the shape is extreme, simplify or re-orient:

- `direction: right` for linked lists / chains (they read left-to-right).
- `direction: down` for short pipelines (hash → index → walk) — but watch the
  height cap; keep it to a few nodes.
- To show *before → after* without a tall two-panel tower, prefer **one
  horizontal panel** with the "after" state drawn as a **dashed** edge, rather
  than stacking two full copies.

The root `<svg>` gets an intrinsic pixel size from `sizeSVG` in `d2.go`; that's
what makes the caps behave. If you ever change d2 render options, re-check that
diagrams don't stretch to full width.

## Theming — emphasize with stroke, not fill

The site themes via a `.dark` class, and each diagram is rendered in a light
theme (`NeutralDefault`) and a dark theme (`DarkMauve`). The trap:

- **Do NOT emphasize a node with a hardcoded `style.fill`.** A fixed fill
  color stays put in both SVGs, but the theme flips the *text* color — so your
  label washes out (light text on a light fill) in one theme.
- **DO emphasize with a colored border:** `style.stroke` + `style.stroke-width`.
  The node keeps the theme's default fill and text (always legible), and the
  border carries the emphasis in both themes.

Conventions used in `build-a-hashmap-c.md` (reuse them for consistency):

| Meaning              | Style                                                        |
|----------------------|-------------------------------------------------------------|
| the node to remove   | `style.stroke: "#dc2626"; style.stroke-width: 3` (red)      |
| a pointer variable   | `shape: oval; style.stroke: "#d97706"` (amber)              |
| a freed / gone node  | `style.stroke-dash: 4; style.font-color: "#9ca3af"` (grey)  |
| an "after" pointer   | edge `style.stroke-dash: 4` (dashed), red if it's a rewrite |

## Stepped (click-through) diagrams

A ```d2 block using D2's native `steps:` composition renders as a
**click-through stepper**: one SVG frame per board, with CSS-only Back/Next
buttons (hidden radio inputs — no JS; see `.d2-steps` in `assets/input.css`).
Use it whenever the point is *how an algorithm proceeds* (a partition sweep, a
sift-up, BFS rings) rather than a static structure.

```d2
direction: right
arr: "…starting state…"
steps: {
  "compare 5 and 3": { arr.style.stroke: "#dc2626" }
  "swap":            { ... }
}
```

- The **root board** (content above `steps:`) is frame 1 — the starting
  state. Each step **inherits cumulatively** from the previous frame and
  states only the delta.
- The **step key becomes the frame's caption** (shown next to the "2 / 5"
  counter) — write keys as short human phrases: `"compare 5 and 3"`, not `s2`.
- **Keep the node/edge structure identical across steps; change only styles
  and labels.** Same structure → same layout → frames don't jump around when
  the reader clicks through. Adding/removing nodes mid-sequence relayouts
  the whole frame and everything shifts.
- Max **12 frames** (`maxStepFrames` in `internal/markdown/d2.go`); ingest
  errors above that. 4–8 is the sweet spot.
- Every frame obeys the same sizing caps as a static diagram; `preview.sh`
  writes numbered PNG pairs (`light-1.png` …) — **read every frame** in at
  least one theme, and frame 1 in both.
- `layers:` / `scenarios:` are rejected at ingest; only `steps:` is
  supported in lessons.

## D2 idioms you'll actually use

- **Struct / array / link-cell:** `sql_table`.
  ```d2
  m: "struct hashmap" {
    shape: sql_table
    buckets: "hm_entry ** — chain heads"
    nbuckets: "size_t"
    len: "size_t"
  }
  ```
  Edges can attach to a specific row: `buckets.1 -> node`. Use a `next` row to
  make a *link* into something a pointer can point at (`pp -> front.next`).
- **Chain:** `a -> b -> c -> nil`, with `nil: "∅" { shape: text }` for the
  null terminator.
- **Flow step / terminal:** plain box for a step; `{ shape: oval }` for the
  entry/exit of a pipeline.
- **Layout:** `direction: right|down`; `grid-rows: N` / `grid-columns: N` to
  arrange sibling containers without connecting edges.
- Quote any label containing `:` `"` or `->` (e.g. `x: "\"ada\" = 0"`).

Keep labels short — every label competes for space and shrinks the figure.

## Gotchas

- **Invalid D2 fails ingest** (by design) with a `line:col` message — a bad
  diagram won't silently disappear; `coursecheck` / `duckserver seed` will
  error. Fix the source.
- **rsvg-convert renders the intrinsic size**, the browser renders the capped
  size. Always judge with `preview.sh` (which applies the caps), not a bare
  render.
- **`make dev` won't re-seed a non-empty DB** — you must re-seed the course to
  see edits (see step 6).
- **`make seed` is currently broken** (points at the removed `cmd/getcracked`).
  Use `go run ./cmd/duckserver seed courses/<file>.md` directly.
