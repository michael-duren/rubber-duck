package httpapi

import "net/http"

// export returns every live variant's and learning path's source document
// in one response. It's what keeps the repo's courses/ and paths/
// directories faithful mirrors of the database: a scheduled GitHub Action
// fetches this (no credentials — content is public) and opens a PR when
// the mirror has drifted. See scripts/export-courses.sh.
func (h *handlers) export(w http.ResponseWriter, r *http.Request) {
	variants, err := h.store.ListVariantSources(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paths, err := h.store.ListPathSources(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	type item struct {
		Course   string `json:"course"`
		Language string `json:"language"`
		Version  int    `json:"version"`
		Markdown string `json:"markdown"`
	}
	type pathItem struct {
		Path     string `json:"path"`
		Version  int    `json:"version"`
		Markdown string `json:"markdown"`
	}
	items := make([]item, len(variants))
	for i, v := range variants {
		items[i] = item{v.CourseSlug, v.Language, v.Version, v.SourceMD}
	}
	pathItems := make([]pathItem, len(paths))
	for i, p := range paths {
		pathItems[i] = pathItem{p.Slug, p.Version, p.SourceMD}
	}
	writeJSON(w, http.StatusOK, map[string]any{"variants": items, "paths": pathItems})
}
