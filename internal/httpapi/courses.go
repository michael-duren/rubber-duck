package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mduren/getcracked/internal/domain"
	"github.com/mduren/getcracked/internal/ingest"
)

const maxDocumentBytes = 2 << 20 // 2 MiB per course document

type variantSummaryJSON struct {
	Language    string `json:"language"`
	Version     int    `json:"version"`
	Lessons     int    `json:"lessons"`
	Challenges  int    `json:"challenges"`
	TotalPoints int    `json:"total_points"`
	UpdatedAt   string `json:"updated_at"`
}

// putVariant is the core agent endpoint: idempotent upsert of one
// course-language variant from a markdown document.
func (h *handlers) putVariant(w http.ResponseWriter, r *http.Request) {
	slug, language := r.PathValue("slug"), r.PathValue("language")

	var body struct {
		Markdown string `json:"markdown"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDocumentBytes))
	if err := dec.Decode(&body); err != nil || body.Markdown == "" {
		writeError(w, http.StatusBadRequest, "bad_request", `body must be JSON: {"markdown": "..."}`, nil)
		return
	}

	src := []byte(body.Markdown)
	res, err := ingest.Parse(src)
	if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		details := make([]Detail, len(verr.Problems))
		for i, p := range verr.Problems {
			details[i] = Detail{Line: p.Line, Message: p.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_course_markdown",
			fmt.Sprintf("%d problems found", len(details)), details)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if res.Course.Course != slug || res.Course.Language != language {
		writeError(w, http.StatusConflict, "slug_mismatch",
			fmt.Sprintf("URL names %s/%s but frontmatter says %s/%s",
				slug, language, res.Course.Course, res.Course.Language), nil)
		return
	}

	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	version, err := h.store.UpsertVariant(r.Context(), course, variant)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	status := http.StatusOK
	if version == 1 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"course":       course.Slug,
		"language":     variant.Language,
		"version":      version,
		"lessons":      len(variant.Lessons),
		"challenges":   variant.ChallengeCount(),
		"total_points": variant.TotalPoints(),
	})
}

func (h *handlers) listCourses(w http.ResponseWriter, r *http.Request) {
	courses, err := h.store.ListCourses(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	type item struct {
		Slug          string   `json:"slug"`
		Title         string   `json:"title"`
		DurationHours float64  `json:"duration_hours,omitempty"`
		Tags          []string `json:"tags"`
		Languages     []string `json:"languages"`
		UpdatedAt     string   `json:"updated_at"`
	}
	items := make([]item, len(courses))
	for i, c := range courses {
		items[i] = item{c.Slug, c.Title, c.DurationHours, c.Tags, c.Languages, c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")}
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": items})
}

func (h *handlers) getCourse(w http.ResponseWriter, r *http.Request) {
	course, variants, err := h.store.CourseBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such course", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	vs := make([]variantSummaryJSON, len(variants))
	for i, v := range variants {
		vs[i] = variantSummaryJSON{v.Language, v.Version, v.Lessons, v.Challenges, v.TotalPoints, v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug":             course.Slug,
		"title":            course.Title,
		"description":      course.DescriptionMD,
		"duration_hours":   course.DurationHours,
		"tags":             course.Tags,
		"extended_reading": course.ExtendedReading,
		"variants":         vs,
	})
}

func (h *handlers) getVariantSource(w http.ResponseWriter, r *http.Request) {
	src, err := h.store.VariantSource(r.Context(), r.PathValue("slug"), r.PathValue("language"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such course variant", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"markdown": src})
}

func (h *handlers) deleteCourse(w http.ResponseWriter, r *http.Request) {
	h.deleted(w, r, h.store.DeleteCourse(r.Context(), r.PathValue("slug")))
}

func (h *handlers) deleteVariant(w http.ResponseWriter, r *http.Request) {
	h.deleted(w, r, h.store.DeleteVariant(r.Context(), r.PathValue("slug"), r.PathValue("language")))
}

func (h *handlers) deleted(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "nothing to delete", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"tags": tags})
}
