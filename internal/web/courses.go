package web

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/mduren/getcracked/internal/domain"
	"github.com/mduren/getcracked/internal/web/views"
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
	h.render(w, r, views.Course(currentUser(r), course, variants))
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
	h.render(w, r, views.Variant(currentUser(r), course, variant))
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
			h.render(w, r, views.Lesson(currentUser(r), course, variant, l, next))
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
	h.render(w, r, views.Final(currentUser(r), course, variant))
}
