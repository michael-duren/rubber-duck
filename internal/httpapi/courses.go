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
		// ExpectedVersion enables optimistic concurrency for human
		// (gc_u_ user-token) callers only — see currentUser handling
		// below. An agent-key caller may send it too; it's simply
		// ignored, matching that path's existing unversioned behavior.
		ExpectedVersion *int `json:"expected_version,omitempty"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDocumentBytes))
	if err := dec.Decode(&body); err != nil {
		// A valid-but-huge body hits MaxBytesReader's limit and would
		// otherwise surface as a baffling "must be JSON" 400.
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
	if _, ok := errors.AsType[*markdown.DiagramError](err); ok {
		// A ```d2 fence that doesn't compile is the document's problem,
		// not the server's: report it like any other validation failure
		// (the wrapped error names the lesson/challenge and d2's line:col).
		writeError(w, http.StatusUnprocessableEntity, "invalid_course_markdown", err.Error(), nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// A human editor (authenticated via a gc_u_ user token, see
	// requireKey) is attributed and may opt into the same optimistic
	// concurrency check the web editor uses. An agent-key caller has no
	// currentUser and stays unattributed and unversioned (see issue #36's
	// scope) — any expected_version it sent is ignored, not validated.
	var editedBy *int64
	var expectedVersion *int
	if user := currentUser(r); user != nil {
		editedBy = &user.ID
		expectedVersion = body.ExpectedVersion
	}

	version, err := h.store.UpsertVariant(r.Context(), course, variant, editedBy, expectedVersion)
	if errors.Is(err, domain.ErrVersionConflict) {
		writeError(w, http.StatusConflict, "version_conflict",
			"the variant has changed since your expected_version — re-fetch it and reapply your changes before pushing", nil)
		return
	}
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
	// version lets a human caller round-trip it back as expected_version
	// on a subsequent PUT (see putVariant), the same optimistic-concurrency
	// pattern the web editor's hidden form field already uses.
	src, version, err := h.store.VariantSource(r.Context(), r.PathValue("slug"), r.PathValue("language"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such course variant", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"markdown": src, "version": version})
}

type challengeJSON struct {
	LessonSlug string `json:"lesson_slug"`
	// LessonNumber is the lesson's 1-based position among ALL the variant's
	// lessons — including lessons with no challenges — so clients can label
	// challenges with the same numbers the lesson list shows. 0 for the
	// final challenge, which belongs to no lesson.
	LessonNumber int    `json:"lesson_number"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Points       int    `json:"points"`
	StarterCode  string `json:"starter_code"`
	TestCode     string `json:"test_code"`
}

// listChallenges is the public (unauthenticated) endpoint local test runs
// use to fetch starter and test code: challenge tests aren't secret here.
func (h *handlers) listChallenges(w http.ResponseWriter, r *http.Request) {
	_, variant, err := h.store.VariantDetail(r.Context(), r.PathValue("slug"), r.PathValue("language"))
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such course variant", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	items := make([]challengeJSON, 0, variant.ChallengeCount())
	for i, l := range variant.Lessons {
		for _, c := range l.Challenges {
			items = append(items, challengeJSON{l.Slug, i + 1, c.Slug, c.Title, c.Points, c.StarterCode, c.TestCode})
		}
	}
	items = append(items, challengeJSON{"", 0, variant.Final.Slug, variant.Final.Title, variant.Final.Points, variant.Final.StarterCode, variant.Final.TestCode})
	writeJSON(w, http.StatusOK, map[string]any{"challenges": items})
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
