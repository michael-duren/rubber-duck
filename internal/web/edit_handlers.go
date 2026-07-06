package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// editVariantPage renders the raw markdown for a course variant in an
// editable textarea. Reuses store.VariantSource, the same lookup the agent
// API's GET /courses/{slug}/variants/{language} uses to round-trip a
// document. The version it was loaded at rides along in a hidden form field
// (see views.EditVariant) so saveVariant can detect a concurrent edit.
func (h *handlers) editVariantPage(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	src, version, err := h.courses.VariantSource(r.Context(), slug, lang)
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// Fetched up front so the warning (issue #37) is visible before the user
	// ever clicks Save, not just after a rejected first attempt; saveVariant
	// still rechecks this at submit time rather than trusting it.
	subCount, err := h.courses.VariantSubmissionCount(r.Context(), slug, lang)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.EditVariant(currentUser(r), slug, lang, src, version, "", nil, subCount))
}

// versionConflictMsg is shown both by the pre-write version peek and by
// UpsertVariant's own conflict result (see saveVariant) — same wording
// either way, since to the user it's the same situation either time.
const versionConflictMsg = "Someone else changed this since you opened it — reload to see their version before saving over it."

// saveVariant runs a submitted markdown document through the same
// ingest.Parse/ToDomain/store.UpsertVariant path the agent API's putVariant
// uses (see internal/httpapi/courses.go), attributing the write to the
// logged-in user instead of leaving it unattributed (agent writes pass nil),
// and passing through the version the form was loaded at so a concurrent
// edit is rejected instead of silently overwritten (issue #36).
//
// On any failure — a missing/invalid version, validation problems, a
// slug/language mismatch against the URL, a version conflict, or an
// unconfirmed destructive save (issue #37) — the edit page is re-rendered
// with exactly what the user submitted, never a re-fetch from storage, so a
// failed save never discards in-progress edits.
//
// Two independent checks can both apply to the same submit: a stale version
// and outstanding submissions needing confirmation. Version conflict wins —
// checked first, via the VariantSource peek below — since it means this
// submit isn't even looking at the current content, making a confirmation
// prompt about deleting submissions attached to that stale view moot. That
// peek read is only for choosing which message to show first; UpsertVariant's
// own atomic WHERE-clause check (unchanged from issue #36) remains the real
// guard against a race between this read and the write.
func (h *handlers) saveVariant(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	markdown := r.FormValue("markdown")
	src := []byte(markdown)

	// The hidden "version" field always round-trips from views.EditVariant;
	// a missing/unparseable value only happens from a stale form or
	// tampering, not normal use, so there's no stored version to preserve
	// here — render with 0 and ask the user to reload.
	version, verr := strconv.Atoi(r.FormValue("version"))
	if verr != nil {
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, 0,
			"Missing or invalid version — reload the page and try again.", nil, 0))
		return
	}

	res, err := ingest.Parse(src)
	if pErr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		problems := make([]views.EditProblem, len(pErr.Problems))
		for i, p := range pErr.Problems {
			problems[i] = views.EditProblem{Line: p.Line, Message: p.Message}
		}
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, version, "", problems, 0))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if res.Course.Course != slug || res.Course.Language != lang {
		msg := fmt.Sprintf("This page edits %s/%s but the document's frontmatter says %s/%s — fix the frontmatter or navigate to the matching course/language before saving.",
			slug, lang, res.Course.Course, res.Course.Language)
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, version, msg, nil, 0))
		return
	}

	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// See the doc comment above for why this runs, and wins, before the
	// submission-count confirmation check.
	if _, storedVersion, srcErr := h.courses.VariantSource(r.Context(), slug, lang); srcErr == nil && storedVersion != version {
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, version, versionConflictMsg, nil, 0))
		return
	}

	// Rechecked fresh, never trusted from the GET page load: a learner could
	// submit between then and now. Re-publishing replaces the variant's
	// lessons/challenges wholesale, cascading a delete of exactly these
	// submissions (see README/UpsertVariant), so a save that would do that
	// requires the confirmed=1 field the "Yes, save anyway" button sends
	// (views.EditVariant) — otherwise stop short of the destructive write
	// and show the warning instead.
	subCount, err := h.courses.VariantSubmissionCount(r.Context(), slug, lang)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if subCount > 0 && r.FormValue("confirmed") != "1" {
		h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, version, "", nil, subCount))
		return
	}

	user := currentUser(r) // non-nil: this route is behind h.requireUser
	if _, err := h.courses.UpsertVariant(r.Context(), course, variant, &user.ID, &version); err != nil {
		if errors.Is(err, domain.ErrVersionConflict) {
			h.render(w, r, views.EditVariant(currentUser(r), slug, lang, markdown, version, versionConflictMsg, nil, 0))
			return
		}
		h.serverError(w, r, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/courses/%s/%s", slug, lang), http.StatusSeeOther)
}
