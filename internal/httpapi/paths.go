package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/markdown"
)

// putPath is the learning-path counterpart of putVariant: idempotent upsert
// of one path from a markdown document. Paths carry no learner data and no
// version counter, so there's no expected_version here — last write wins,
// which matches how rarely and centrally paths change.
func (h *handlers) putPath(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var body struct {
		Markdown string `json:"markdown"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDocumentBytes))
	if err := dec.Decode(&body); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "document_too_large",
				fmt.Sprintf("document exceeds the %d-byte limit", maxDocumentBytes), nil)
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", `body must be JSON: {"markdown": "..."}`, nil)
		return
	}
	if body.Markdown == "" {
		writeError(w, http.StatusBadRequest, "bad_request", `missing or empty "markdown" field`, nil)
		return
	}

	src := []byte(body.Markdown)
	res, err := ingest.ParsePath(src)
	if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		details := make([]Detail, len(verr.Problems))
		for i, p := range verr.Problems {
			details[i] = Detail{Line: p.Line, Message: p.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_path_markdown",
			fmt.Sprintf("%d problems found", len(details)), details)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if res.Path.Path != slug {
		writeError(w, http.StatusConflict, "slug_mismatch",
			fmt.Sprintf("URL names %s but frontmatter says %s", slug, res.Path.Path), nil)
		return
	}

	path, err := ingest.PathToDomain(res, src)
	if _, ok := errors.AsType[*markdown.DiagramError](err); ok {
		// Same contract as putVariant: a ```d2 fence in the overview that
		// doesn't compile is the document's problem, not the server's.
		writeError(w, http.StatusUnprocessableEntity, "invalid_path_markdown", err.Error(), nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	created, err := h.store.UpsertPath(r.Context(), path)
	if unknown, ok := errors.AsType[*domain.UnknownCoursesError](err); ok {
		// From the author's side this is a validation failure: fix the
		// courses list, or publish the missing course first.
		writeError(w, http.StatusUnprocessableEntity, "unknown_course_slugs", unknown.Error(), nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"path":    path.Slug,
		"title":   path.Title,
		"courses": path.CourseSlugs,
	})
}

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
// course list so an agent can see the track order without re-parsing.
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

func (h *handlers) deletePath(w http.ResponseWriter, r *http.Request) {
	h.deleted(w, r, h.store.DeletePath(r.Context(), r.PathValue("slug")))
}
