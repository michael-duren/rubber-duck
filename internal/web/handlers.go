package web

import (
	"embed"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"

	"github.com/michael-duren/rubber-duck/internal/web/views"
)

//go:embed static
var staticFS embed.FS

// CanonicalHost redirects any request whose host carries a "www." prefix to
// the same path on the bare apex. It uses 308 (not 301) so non-GET methods
// keep their method and body — this wraps the agent API too. Requests to the
// apex, localhost, or the *.run.app URL fall straight through, so it's safe
// to mount in front of the whole mux in every environment.
func CanonicalHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apex, ok := strings.CutPrefix(r.Host, "www."); ok {
			http.Redirect(w, r, "https://"+apex+r.RequestURI, http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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

	if sh, err := newStaticHandler(staticFS, "static"); err != nil {
		// Only reachable if the embedded FS is unreadable (a build-time
		// impossibility); fall back to plain, uncompressed serving.
		logger.Error("static handler init failed; serving uncompressed", "err", err)
		mux.Handle("GET /static/", http.FileServerFS(staticFS))
	} else {
		mux.Handle("GET /static/", sh)
	}

	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}", h.homePage)
	pages.HandleFunc("GET /courses", h.catalog)
	pages.HandleFunc("GET /paths", h.pathsPage)
	pages.HandleFunc("GET /paths/{slug}", h.pathPage)
	pages.HandleFunc("GET /about", h.aboutPage)
	pages.HandleFunc("GET /cli", h.cliPage)
	pages.HandleFunc("GET /tokens", h.tokensPage)
	pages.HandleFunc("GET /courses/new", h.requireUser(h.newCoursePage))
	pages.HandleFunc("POST /courses/new", h.requireUser(h.createCourse))
	pages.HandleFunc("GET /courses/{slug}", h.coursePage)
	pages.HandleFunc("GET /courses/{slug}/card.svg", h.courseArt)
	pages.HandleFunc("GET /courses/{slug}/{lang}", h.variantPage)
	pages.HandleFunc("GET /courses/{slug}/{lang}/edit", h.requireUser(h.editVariantPage))
	pages.HandleFunc("POST /courses/{slug}/{lang}/edit", h.requireUser(h.saveVariant))
	pages.HandleFunc("POST /courses/{slug}/{lang}/edit/preview", h.requireUser(h.previewVariant))
	pages.HandleFunc("GET /courses/{slug}/{lang}/lessons/{lesson}", h.lessonPage)
	pages.HandleFunc("GET /courses/{slug}/{lang}/final", h.finalPage)
	pages.HandleFunc("POST /challenges/{id}/submissions", h.requireUser(h.submit))
	pages.HandleFunc("POST /courses/{slug}/{lang}/challenges/{challenge}/submissions", h.requireUser(h.submitBySlug))
	pages.HandleFunc("GET /submissions/{id}", h.requireUser(h.submissionPage))
	pages.HandleFunc("GET /submissions/{id}/fragment", h.requireUser(h.submissionFragment))
	pages.HandleFunc("GET /submissions/{id}/status", h.requireUser(h.submissionStatus))
	pages.HandleFunc("GET /profile", h.requireUser(h.profile))
	pages.HandleFunc("POST /profile/tokens", h.requireUser(h.createUserToken))
	pages.HandleFunc("POST /profile/tokens/{id}/revoke", h.requireUser(h.revokeUserToken))
	pages.HandleFunc("GET /settings", h.requireUser(h.settingsPage))
	pages.HandleFunc("POST /settings", h.requireUser(h.changePassword))
	pages.HandleFunc("GET /signup", h.signupPage)
	pages.HandleFunc("POST /signup", h.signup)
	pages.HandleFunc("GET /login", h.loginPage)
	pages.HandleFunc("POST /login", h.login)
	pages.HandleFunc("POST /logout", h.logout)
	mux.Handle("/", h.withCSRF(h.withUser(pages)))
}

// homePage renders the landing page. It pulls the course list so the
// "fresh from the catalog" strip shows real courses; the strip is capped at
// three and skipped entirely on an empty deployment.
func (h *handlers) homePage(w http.ResponseWriter, r *http.Request) {
	courses, err := h.courses.ListCourses(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if len(courses) > 3 {
		courses = courses[:3]
	}
	progress, err := h.userProgress(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.Home(currentUser(r), courses, resumeTarget(progress), progressBySlug(progress)))
}

func (h *handlers) aboutPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.About(currentUser(r)))
}

// cliPage and tokensPage render the CLI and token docs; the command
// examples hard-code the canonical deployment (https://duckgc.com), which
// is also what the duck CLI targets by default.
func (h *handlers) cliPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.CLI(currentUser(r)))
}

func (h *handlers) tokensPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.Tokens(currentUser(r)))
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
