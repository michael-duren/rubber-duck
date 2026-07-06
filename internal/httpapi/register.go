package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// CourseStore is the slice of the store the agent API needs.
type CourseStore interface {
	// editedBy/expectedVersion are both nil for agent-key callers (writes
	// stay unattributed and unversioned, see issue #36's scope); a human
	// caller authenticated via a gc_u_ user token (see currentUser in
	// middleware.go) is attributed and may pass a non-nil expectedVersion
	// for optimistic concurrency (see internal/store.Store.UpsertVariant's
	// doc comment).
	UpsertVariant(ctx context.Context, course domain.Course, variant domain.Variant, editedBy *int64, expectedVersion *int) (int, error)
	ListCourses(ctx context.Context) ([]domain.CourseSummary, error)
	CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error)
	// VariantSource's int return is the variant's current version;
	// getVariantSource includes it in the GET response so a human caller
	// can round-trip it back as expected_version on a later PUT.
	VariantSource(ctx context.Context, slug, language string) (string, int, error)
	VariantDetail(ctx context.Context, courseSlug, language string) (domain.Course, domain.Variant, error)
	DeleteCourse(ctx context.Context, slug string) error
	DeleteVariant(ctx context.Context, slug, language string) error
	ListTags(ctx context.Context) ([]string, error)
}

type handlers struct {
	store  CourseStore
	logger *slog.Logger
}

// Register mounts the agent API under /api/v1, guarded by bearer auth —
// either an agent API key or a human user token (see requireKey).
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

	// Public: challenge prompts and tests aren't secret on a learning
	// platform, and local test runs need them without a bearer key.
	mux.HandleFunc("GET /api/v1/courses/{slug}/variants/{language}/challenges", h.listChallenges)
}

func (h *handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("api error", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
}
