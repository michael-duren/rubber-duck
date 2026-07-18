package httpapi

import (
	"errors"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// Learning paths are read-only over HTTP: they have no proposal flow, so
// writes happen only through `duckserver seed` (paths/ in the repo is the
// canonical source, unlike the courses/ mirror). Paths carry no learner
// data and no version counter — see store.UpsertPath.

func (h *handlers) listPaths(w http.ResponseWriter, r *http.Request) {
	paths, err := h.store.ListPaths(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	type item struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		CourseCount int    `json:"course_count"`
		UpdatedAt   string `json:"updated_at"`
	}
	items := make([]item, len(paths))
	for i, p := range paths {
		items[i] = item{p.Slug, p.Title, p.CourseCount, p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")}
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": items})
}

// getPath returns the stored markdown for round-tripping, plus the resolved
// course list so a caller can see the track order without re-parsing.
func (h *handlers) getPath(w http.ResponseWriter, r *http.Request) {
	path, _, err := h.store.PathBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such learning path", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"markdown": path.SourceMD,
		"title":    path.Title,
		"courses":  path.CourseSlugs,
	})
}
