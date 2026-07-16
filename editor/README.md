# Challenge editor (CodeMirror 6)

Source for the in-browser challenge editor. Bundled into
`internal/web/static/cm6.js`, which the Go binary embeds via `//go:embed`.

The web app has **no JS bundler** — like `htmx.min.js` and `alpine.min.js`,
`cm6.js` is a **committed, vendored artifact**. CI and `make check` never run
Node. You only need Node here when changing the editor or bumping CodeMirror.

## Rebuild

```sh
make editor-bundle      # from repo root: npm ci && esbuild -> ../internal/web/static/cm6.js
```

or directly:

```sh
cd editor && npm ci && npm run build
```

Requires Node >= 18. Commit the regenerated `internal/web/static/cm6.js`
alongside any change to `src/main.js` or `package.json`.

## What it does

Progressively enhances every `<textarea data-editor>` (rendered by
`ChallengeCard` in `internal/web/views/lesson.templ`) into a CodeMirror 6
editor: syntax highlighting per `data-language` (`go`/`python`/`c`), line
numbers, bracket matching, tab-to-indent, and an optional vim keymap. The
textarea stays in the DOM (hidden) and is kept in sync, so the plain form
POST to `/challenges/{id}/submissions` is unchanged — and if the script never
loads, the textarea still works.

Settings (font size, tab width, line wrap, vim) persist in `localStorage`
under `rd.editor`, the same client-side convention as the dark/light theme
toggle.
