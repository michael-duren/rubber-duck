package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/markdown"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// slugPattern matches the lowercase-kebab-case course slugs used throughout
// the system (courses/*.md file names, agent API paths, URLs here) — the
// "New course" form checks a submitted slug against it before ever handing
// it to ingest.Parse, so a bad slug gets a clear message instead of turning
// into a confusing frontmatter validation error later.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// newCourseLanguages are the languages offered by the "New course" form's
// language <select> — the three toolchains dockergrader/cloudrungrader
// actually support (internal/grader) and README documents as the allowed
// `language:` frontmatter values.
var newCourseLanguages = []string{"go", "python", "c"}

// newCoursePage renders the small entry-point form for starting a brand-new
// course or adding a new language variant to an existing one (issue #38).
// It's reached two ways: empty, from the catalog's "+ New course" button,
// or pre-filled via slug/title/description query params, from an existing
// course page's "+ Add language variant" link — either way this form only
// asks for enough to seed a frontmatter template, not the full document.
func (h *handlers) newCoursePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	h.render(w, r, views.NewCourse(currentUser(r), q.Get("slug"), q.Get("title"), q.Get("language"), q.Get("description"), newCourseLanguages, ""))
}

// createCourse validates the "New course" form and, on success, redirects
// into the same raw-markdown editor used to edit an existing variant
// (editVariantPage/saveVariant) rather than creating anything itself: the
// redirect's new=1 query param tells editVariantPage to seed a template
// instead of 404ing for this not-yet-existing slug/language, and the first
// Save on that page is what actually persists a course/variant row, via the
// ordinary saveVariant path — no separate creation code needed there.
func (h *handlers) createCourse(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.FormValue("slug"))
	title := strings.TrimSpace(r.FormValue("title"))
	language := strings.TrimSpace(r.FormValue("language"))
	description := strings.TrimSpace(r.FormValue("description"))

	var errMsg string
	switch {
	case slug == "" || title == "" || language == "":
		errMsg = "Slug, title, and language are all required."
	case !slugPattern.MatchString(slug):
		errMsg = "Slug must be lowercase letters, numbers, and hyphens only (e.g. intro-to-concurrency)."
	case !slices.Contains(newCourseLanguages, language):
		errMsg = "Language must be one of: " + strings.Join(newCourseLanguages, ", ") + "."
	}
	if errMsg != "" {
		h.render(w, r, views.NewCourse(currentUser(r), slug, title, language, description, newCourseLanguages, errMsg))
		return
	}

	q := url.Values{"new": {"1"}, "title": {title}, "description": {description}}
	http.Redirect(w, r, fmt.Sprintf("/courses/%s/%s/edit?%s", slug, language, q.Encode()), http.StatusSeeOther)
}

// newVariantTemplate builds the minimal valid course document (issue #38)
// used to pre-fill the editor for a slug/language that doesn't exist yet:
// required frontmatter (README "Course document format"), one lesson, and
// one final challenge with Starter/Tests blocks — ingest.Parse requires at
// least those to validate a document, so this passes as-is or with the
// TODOs filled in, never a validation error on an unedited first save.
func newVariantTemplate(slug, language, title, description string) string {
	if title == "" {
		title = slug
	}
	if description == "" {
		description = "TODO: one-paragraph pitch."
	}
	starter, tests := seedCodeStubs(language)
	return "---\n" +
		"course: " + slug + "\n" +
		"title: " + title + "\n" +
		"language: " + language + "\n" +
		"description: " + description + "\n" +
		"---\n\n" +
		"# Lesson: Getting Started {#getting-started}\n\n" +
		"TODO: write this lesson's content.\n\n" +
		"# Final Challenge: TODO {#todo points=10}\n\n" +
		"TODO: describe the challenge.\n\n" +
		"### Starter\n\n" +
		"```" + language + "\n" + starter + "```\n\n" +
		"### Tests\n\n" +
		"```" + language + "\n" + tests + "```\n"
}

// seedCodeStubs returns placeholder Starter/Tests code for the given
// language, matching each toolchain's expected shape (README "Course
// document format") closely enough to be a sane starting point, not a
// working solution — the point is a template that validates, not one that
// grades.
func seedCodeStubs(language string) (starter, tests string) {
	switch language {
	case "go":
		return "package challenge\n\n// TODO: starter code\n", "package challenge\n\n// TODO: tests\n"
	case "python":
		return "# TODO: starter code\n", "# TODO: tests\nfrom solution import example\n"
	case "c":
		return "// TODO: starter code\n", "// TODO: tests\nint main(void) {\n\treturn 0;\n}\n"
	default:
		return "TODO: starter code\n", "TODO: tests\n"
	}
}

// editVariantPage renders the raw markdown for a course variant in an
// editable textarea. Reuses store.VariantSource, the same lookup the agent
// API's GET /courses/{slug}/variants/{language} uses to round-trip a
// document. The version it was loaded at rides along in a hidden form field
// (see views.EditVariant) so saveVariant can detect a concurrent edit.
//
// A missing variant is a 404, same as before issue #38, UNLESS new=1 is
// present in the query string: that only arrives via createCourse's
// redirect, so it means the user deliberately started this slug/language
// through the "New course" form, and the page should seed a template
// instead of 404ing. Without new=1, a mistyped/nonexistent URL still 404s —
// organic navigation to a bad slug/lang must not silently become a "create"
// page.
func (h *handlers) editVariantPage(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	src, version, err := h.courses.VariantSource(r.Context(), slug, lang)
	if errors.Is(err, domain.ErrNotFound) {
		if r.URL.Query().Get("new") == "1" {
			q := r.URL.Query()
			tmpl := newVariantTemplate(slug, lang, q.Get("title"), q.Get("description"))
			h.render(w, r, views.EditVariant(currentUser(r), slug, lang, tmpl, 0, "", nil, 0))
			return
		}
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

// previewVariant renders whatever markdown a user currently has in the
// editor's textarea (not necessarily saved, not necessarily even valid) as
// an HTML fragment for the editor's live preview pane (issue #39). It's
// reached by the page's own inline script, POSTing the textarea's current
// value on a debounce, and gated behind h.requireUser same as the edit page
// itself and saveVariant — an anonymous visitor gets no preview endpoint any
// more than they get the editor that calls it.
//
// This intentionally shares almost nothing with saveVariant: no
// ingest.Parse/ToDomain, no store write, no version or submission-count
// check. The preview is read-only and side-effect-free, so it can't
// interfere with — or be blocked behind — those checks, and a save can
// still succeed (or correctly fail) independent of whatever the preview
// pane is currently showing.
//
// The submitted markdown is a full course document, frontmatter included,
// which markdown.ToHTML doesn't know how to render as anything but stray
// text — so the leading frontmatter block is stripped first (see
// markdown.StripFrontmatter) before rendering the remainder, the same
// lesson/challenge prose a learner would actually see on the rendered
// course page.
func (h *handlers) previewVariant(w http.ResponseWriter, r *http.Request) {
	body := markdown.StripFrontmatter([]byte(r.FormValue("markdown")))
	html, err := markdown.ToHTML(body)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
