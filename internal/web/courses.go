package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// CourseReader is the slice of the store the course pages need. Despite the
// name it also carries the two write operations the web markdown editor
// needs (VariantSource/UpsertVariant): the read pages and the editor share
// one *store.Store, so one interface keeps the wiring in Register simple.
type CourseReader interface {
	ListCourses(ctx context.Context) ([]domain.CourseSummary, error)
	CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error)
	VariantDetail(ctx context.Context, courseSlug, language string) (domain.Course, domain.Variant, error)

	// VariantSource returns the raw stored markdown for the edit form, plus
	// its current version so the form can carry it in a hidden field and
	// detect a concurrent edit on save (see UpsertVariant's expectedVersion).
	VariantSource(ctx context.Context, courseSlug, language string) (string, int, error)
	// UpsertVariant persists a parsed course/variant. editedBy is the acting
	// user's ID for human web edits, nil for agent/system writes.
	// expectedVersion, if non-nil, rejects the write with
	// domain.ErrVersionConflict when the stored version has moved on since
	// the caller loaded it (optimistic concurrency for the web editor); the
	// web save handler always passes it, other callers pass nil.
	UpsertVariant(ctx context.Context, course domain.Course, variant domain.Variant, editedBy *int64, expectedVersion *int) (int, error)
	// VariantSubmissionCount reports how many submissions exist against this
	// variant's challenges — UpsertVariant's wholesale replace cascades a
	// delete of exactly these, so the edit page uses it to warn before that
	// happens (issue #37).
	VariantSubmissionCount(ctx context.Context, courseSlug, language string) (int, error)
}

func (h *handlers) catalog(w http.ResponseWriter, r *http.Request) {
	courses, err := h.courses.ListCourses(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// tag repeats: ?tag=web&tag=api narrows to courses carrying every
	// selected tag (multi-select in the catalog's --tag dropdown).
	tags, lang := r.URL.Query()["tag"], r.URL.Query().Get("lang")
	tags = slices.DeleteFunc(tags, func(t string) bool { return t == "" })
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	var allTags, allLangs []string
	filtered := courses[:0:0]
	for _, c := range courses {
		for _, t := range c.Tags {
			if !slices.Contains(allTags, t) {
				allTags = append(allTags, t)
			}
		}
		for _, l := range c.Languages {
			if !slices.Contains(allLangs, l) {
				allLangs = append(allLangs, l)
			}
		}
		if hasAllTags(c, tags) && (lang == "" || slices.Contains(c.Languages, lang)) && matchesQuery(c, query) {
			filtered = append(filtered, c)
		}
	}
	slices.Sort(allTags)
	slices.Sort(allLangs)

	// htmx live-search swaps only the results fragment; a history restore
	// (back button after htmx pruned its cache) still needs the full page.
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		h.render(w, r, views.CatalogResults(filtered, query, tags, lang))
		return
	}
	h.render(w, r, views.Catalog(currentUser(r), filtered, allTags, allLangs, tags, lang, query))
}

// hasAllTags reports whether the course carries every selected tag; an empty
// selection matches everything.
func hasAllTags(c domain.CourseSummary, tags []string) bool {
	for _, t := range tags {
		if !slices.Contains(c.Tags, t) {
			return false
		}
	}
	return true
}

// matchesQuery reports whether the search box text hits a course's title,
// slug, tags, or languages, case-insensitively. The slug matters because
// titles and slugs word-break differently ("Hash Map" vs build-a-hashmap).
// An empty query matches everything.
func matchesQuery(c domain.CourseSummary, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(c.Title), q) || strings.Contains(c.Slug, q) {
		return true
	}
	for _, t := range c.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	for _, l := range c.Languages {
		if strings.Contains(strings.ToLower(l), q) {
			return true
		}
	}
	return false
}

func (h *handlers) coursePage(w http.ResponseWriter, r *http.Request) {
	course, variants, err := h.courses.CourseBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	scores := map[string]domain.CourseScore{} // by language, this course only
	if user := currentUser(r); user != nil {
		all, err := h.submissions.UserCourseScores(r.Context(), user.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		for _, s := range all {
			if s.CourseSlug == course.Slug {
				scores[s.Language] = s
			}
		}
	}
	h.render(w, r, views.Course(currentUser(r), course, variants, scores))
}

func (h *handlers) variantPage(w http.ResponseWriter, r *http.Request) {
	course, variant, err := h.courses.VariantDetail(r.Context(), r.PathValue("slug"), r.PathValue("lang"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	completed, err := h.completedChallenges(w, r, variant.ID)
	if err != nil {
		return
	}
	h.render(w, r, views.Variant(currentUser(r), course, variant, completed))
}

// completedChallenges is nil for anonymous visitors (no progress to mark).
func (h *handlers) completedChallenges(w http.ResponseWriter, r *http.Request, variantID int64) (map[int64]bool, error) {
	user := currentUser(r)
	if user == nil {
		return nil, nil
	}
	completed, err := h.submissions.CompletedChallenges(r.Context(), user.ID, variantID)
	if err != nil {
		h.serverError(w, r, err)
		return nil, err
	}
	return completed, nil
}

func (h *handlers) lessonPage(w http.ResponseWriter, r *http.Request) {
	course, variant, err := h.courses.VariantDetail(r.Context(), r.PathValue("slug"), r.PathValue("lang"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	slug := r.PathValue("lesson")
	for i, l := range variant.Lessons {
		if l.Slug == slug {
			var next *domain.Lesson
			if i+1 < len(variant.Lessons) {
				next = &variant.Lessons[i+1]
			}

			// Fetch latest submission codes and submission history for logged-in users
			latestCodeByChallenge := make(map[int64]string)
			submissionsByChallenge := make(map[int64][]domain.Submission)
			user := currentUser(r)
			if user != nil {
				codes, err := h.submissions.LatestSubmissionCodesByVariant(r.Context(), user.ID, variant.ID)
				if err != nil {
					h.serverError(w, r, err)
					return
				}
				latestCodeByChallenge = codes

				// Fetch submission history for each challenge
				for _, c := range l.Challenges {
					subs, err := h.submissions.SubmissionsForChallenge(r.Context(), user.ID, c.ID)
					if err != nil {
						h.serverError(w, r, err)
						return
					}
					submissionsByChallenge[c.ID] = subs
				}
			}

			h.render(w, r, views.Lesson(currentUser(r), course, variant, l, next, latestCodeByChallenge, submissionsByChallenge))
			return
		}
	}
	http.NotFound(w, r)
}

func (h *handlers) finalPage(w http.ResponseWriter, r *http.Request) {
	course, variant, err := h.courses.VariantDetail(r.Context(), r.PathValue("slug"), r.PathValue("lang"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// Fetch latest submission code and submission history for logged-in users
	latestCode := ""
	var submissions []domain.Submission
	user := currentUser(r)
	if user != nil {
		codes, err := h.submissions.LatestSubmissionCodesByVariant(r.Context(), user.ID, variant.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		latestCode = codes[variant.Final.ID]

		subs, err := h.submissions.SubmissionsForChallenge(r.Context(), user.ID, variant.Final.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		submissions = subs
	}

	h.render(w, r, views.Final(currentUser(r), course, variant, latestCode, submissions))
}
