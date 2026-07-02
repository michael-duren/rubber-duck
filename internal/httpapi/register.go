package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mduren/getcracked/internal/domain"
)

// CourseStore is the slice of the store the agent API needs.
type CourseStore interface {
	UpsertVariant(ctx context.Context, course domain.Course, variant domain.Variant) (int, error)
	ListCourses(ctx context.Context) ([]domain.CourseSummary, error)
	CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error)
	VariantSource(ctx context.Context, slug, language string) (string, error)
	DeleteCourse(ctx context.Context, slug string) error
	DeleteVariant(ctx context.Context, slug, language string) error
	ListTags(ctx context.Context) ([]string, error)
}

type handlers struct {
	store  CourseStore
	logger *slog.Logger
}

// Register mounts the agent API under /api/v1, guarded by API-key auth.
func Register(mux *http.ServeMux, logger *slog.Logger, keys KeyStore, store CourseStore) {
	h := &handlers{store: store, logger: logger}

	api := http.NewServeMux()
	api.HandleFunc("PUT /api/v1/courses/{slug}/variants/{language}", h.putVariant)
	api.HandleFunc("GET /api/v1/courses/{slug}/variants/{language}", h.getVariantSource)
	api.HandleFunc("DELETE /api/v1/courses/{slug}/variants/{language}", h.deleteVariant)
	api.HandleFunc("GET /api/v1/courses/{slug}", h.getCourse)
	api.HandleFunc("DELETE /api/v1/courses/{slug}", h.deleteCourse)
	api.HandleFunc("GET /api/v1/courses", h.listCourses)
	api.HandleFunc("GET /api/v1/tags", h.listTags)

	mux.Handle("/api/v1/", requireKey(keys, api))
}

func (h *handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("api error", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
}
