package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// editVariantPage renders the raw markdown for a course variant in an
// editable textarea. Reuses store.VariantSource, the same lookup the agent
// API's GET /courses/{slug}/variants/{language} uses to round-trip a
// document.
func (h *handlers) editVariantPage(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	src, err := h.courses.VariantSource(r.Context(), slug, lang)
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.EditVariant(currentUser(r), slug, lang, src, "", nil))
}

// saveVariant runs a submitted markdown document through the same
// ingest.Parse/ToDomain/store.UpsertVariant path the agent API's putVariant
// uses (see internal/httpapi/courses.go), attributing the write to the
// logged-in user instead of leaving it unattributed (agent writes pass nil).
//
// On any failure — validation problems or a slug/language mismatch against
// the URL — the edit page is re-rendered with exactly what the user
// submitted, never a re-fetch from storage, so a failed save never
// discards in-progress edits.
func (h *handlers) saveVariant(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	markdown := r.FormValue("markdown")
	src := []byte(markdown)

	res, err := ingest.Parse(src)
	if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		problems := make([]views.EditProblem, len(verr.Problems))
		for i, p := range verr.Problems {
			problems[i] = views.EditProblem{Line: p.Line, Message: p.Message}
		}
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, "", problems))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if res.Course.Course != slug || res.Course.Language != lang {
		msg := fmt.Sprintf("This page edits %s/%s but the document's frontmatter says %s/%s — fix the frontmatter or navigate to the matching course/language before saving.",
			slug, lang, res.Course.Course, res.Course.Language)
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, msg, nil))
		return
	}

	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	user := currentUser(r) // non-nil: this route is behind h.requireUser
	if _, err := h.courses.UpsertVariant(r.Context(), course, variant, &user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/courses/%s/%s", slug, lang), http.StatusSeeOther)
}
