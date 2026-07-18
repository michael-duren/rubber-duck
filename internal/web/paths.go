package web

import (
	"errors"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

func (h *handlers) pathsPage(w http.ResponseWriter, r *http.Request) {
	paths, err := h.courses.ListPaths(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.Paths(currentUser(r), paths))
}

func (h *handlers) pathPage(w http.ResponseWriter, r *http.Request) {
	path, courses, err := h.courses.PathBySlug(r.Context(), r.PathValue("slug"))
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	progress, err := h.userProgress(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	bySlug := progressBySlug(progress)
	h.render(w, r, views.Path(currentUser(r), path, courses, bySlug, completedCourses(courses, bySlug)))
}

// completedCourses counts the path's member courses the user has fully
// finished (every lesson of their most recently worked variant done).
func completedCourses(courses []domain.CourseSummary, progress map[string]domain.VariantProgress) int {
	done := 0
	for _, c := range courses {
		if p, ok := progress[c.Slug]; ok && p.Complete() {
			done++
		}
	}
	return done
}
