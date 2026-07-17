package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
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

// parseCourseDoc runs a submitted markdown document through ingest.Parse,
// writing the API's standard error responses (422 with line-numbered
// details for validation problems, 500 otherwise) itself. Callers get
// (nil, false) when a response has already been written.
func (h *handlers) parseCourseDoc(w http.ResponseWriter, r *http.Request, src []byte) (*ingest.Result, bool) {
	res, err := ingest.Parse(src)
	if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		details := make([]Detail, len(verr.Problems))
		for i, p := range verr.Problems {
			details[i] = Detail{Line: p.Line, Message: p.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_course_markdown",
			fmt.Sprintf("%d problems found", len(details)), details)
		return nil, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return nil, false
	}
	return res, true
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
