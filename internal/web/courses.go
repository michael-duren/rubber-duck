package web

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// CourseReader is the slice of the store the course pages need.
type CourseReader interface {
	ListCourses(ctx context.Context) ([]domain.CourseSummary, error)
	CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error)
	VariantDetail(ctx context.Context, courseSlug, language string) (domain.Course, domain.Variant, error)
}

func (h *handlers) catalog(w http.ResponseWriter, r *http.Request) {
	courses, err := h.courses.ListCourses(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	tag, lang := r.URL.Query().Get("tag"), r.URL.Query().Get("lang")
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
		if (tag == "" || slices.Contains(c.Tags, tag)) && (lang == "" || slices.Contains(c.Languages, lang)) {
			filtered = append(filtered, c)
		}
	}
	slices.Sort(allTags)
	slices.Sort(allLangs)

	h.render(w, r, views.Catalog(currentUser(r), filtered, allTags, allLangs, tag, lang))
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
