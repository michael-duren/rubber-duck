package web

import (
	"embed"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

//go:embed static
var staticFS embed.FS

type handlers struct {
	store       AuthStore
	courses     CourseReader
	submissions SubmissionStore
	enqueuer    Enqueuer
	logger      *slog.Logger
}

// Register mounts all user-facing routes on mux.
func Register(mux *http.ServeMux, logger *slog.Logger, store AuthStore, courses CourseReader, submissions SubmissionStore, enqueuer Enqueuer) {
	h := &handlers{store: store, courses: courses, submissions: submissions, enqueuer: enqueuer, logger: logger}

	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}", h.catalog)
	pages.HandleFunc("GET /courses/{slug}", h.coursePage)
	pages.HandleFunc("GET /courses/{slug}/{lang}", h.variantPage)
	pages.HandleFunc("GET /courses/{slug}/{lang}/lessons/{lesson}", h.lessonPage)
	pages.HandleFunc("GET /courses/{slug}/{lang}/final", h.finalPage)
	pages.HandleFunc("POST /challenges/{id}/submissions", h.requireUser(h.submit))
	pages.HandleFunc("GET /submissions/{id}", h.requireUser(h.submissionPage))
	pages.HandleFunc("GET /submissions/{id}/fragment", h.requireUser(h.submissionFragment))
	pages.HandleFunc("GET /profile", h.requireUser(h.profile))
	pages.HandleFunc("GET /signup", h.signupPage)
	pages.HandleFunc("POST /signup", h.signup)
	pages.HandleFunc("GET /login", h.loginPage)
	pages.HandleFunc("POST /login", h.login)
	pages.HandleFunc("POST /logout", h.logout)
	mux.Handle("/", h.withUser(pages))
}

func (h *handlers) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		h.logger.Error("render", "path", r.URL.Path, "err", err)
	}
}

func (h *handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("server error", "path", r.URL.Path, "err", err)
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}
