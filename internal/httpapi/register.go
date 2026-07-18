package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// CourseStore is the slice of the store the read API needs.
type CourseStore interface {
	ListCourses(ctx context.Context) ([]domain.CourseSummary, error)
	CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error)
	// VariantSource's int return is the variant's current version; a caller
	// authoring a proposal records it to know what base they edited against.
	VariantSource(ctx context.Context, slug, language string) (string, int, error)
	VariantDetail(ctx context.Context, courseSlug, language string) (domain.Course, domain.Variant, error)
	ListVariantSources(ctx context.Context) ([]domain.VariantExport, error)
	ListTags(ctx context.Context) ([]string, error)

	// Learning paths: ordered tracks of courses. Read-only here — path
	// writes go through `duckserver seed`, not HTTP (see paths.go).
	ListPaths(ctx context.Context) ([]domain.PathSummary, error)
	PathBySlug(ctx context.Context, slug string) (domain.LearningPath, []domain.CourseSummary, error)
}

// ProposalStore is the slice of the store the proposal API needs. Reviewing
// and publishing are web-only flows; the CLI only authors.
type ProposalStore interface {
	CreateProposal(ctx context.Context, proposerID int64, courseSlug, language, title, summary, markdown string) (domain.Proposal, error)
	UpdateProposalMarkdown(ctx context.Context, proposalID, proposerID int64, title, summary, markdown string) (domain.Proposal, error)
	ProposalByID(ctx context.Context, id int64) (domain.Proposal, error)
	ListProposalsByUser(ctx context.Context, userID int64) ([]domain.Proposal, error)
	WithdrawProposal(ctx context.Context, proposalID, proposerID int64) error
}

type handlers struct {
	store     CourseStore
	proposals ProposalStore
	logger    *slog.Logger
}

// Register mounts the API under /api/v1. Reads are public — course content
// is public on the web, and credential-free reads are what let the
// repo-mirror sync (see /api/v1/export) run from a plain GitHub Action.
// Proposal routes require a user token.
func Register(mux *http.ServeMux, logger *slog.Logger, users UserStore, store CourseStore, proposals ProposalStore) {
	h := &handlers{store: store, proposals: proposals, logger: logger}

	mux.HandleFunc("GET /api/v1/courses", h.listCourses)
	mux.HandleFunc("GET /api/v1/courses/{slug}", h.getCourse)
	mux.HandleFunc("GET /api/v1/courses/{slug}/variants/{language}", h.getVariantSource)
	mux.HandleFunc("GET /api/v1/courses/{slug}/variants/{language}/challenges", h.listChallenges)
	mux.HandleFunc("GET /api/v1/tags", h.listTags)
	mux.HandleFunc("GET /api/v1/export", h.export)

	// Learning paths are read-only over HTTP: they have no proposal flow,
	// so writes happen only via `duckserver seed` (paths/ in the repo is
	// canonical, unlike the courses/ mirror).
	mux.HandleFunc("GET /api/v1/paths", h.listPaths)
	mux.HandleFunc("GET /api/v1/paths/{slug}", h.getPath)

	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/proposals", h.createProposal)
	api.HandleFunc("GET /api/v1/proposals", h.listMyProposals)
	api.HandleFunc("GET /api/v1/proposals/{id}", h.getProposal)
	api.HandleFunc("PUT /api/v1/proposals/{id}", h.updateProposal)
	api.HandleFunc("POST /api/v1/proposals/{id}/withdraw", h.withdrawProposal)
	mux.Handle("/api/v1/proposals", requireUser(logger, users, api))
	mux.Handle("/api/v1/proposals/", requireUser(logger, users, api))
}

func (h *handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("api error", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
}
