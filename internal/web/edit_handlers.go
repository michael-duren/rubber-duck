package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/domain"
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
	case slug == "new":
		// slugPattern happily matches the literal "new", but GET
		// /courses/new is registered as its own literal route (the "New
		// course" form) alongside the GET /courses/{slug} wildcard, and
		// ServeMux always prefers a literal match over a wildcard one — so a
		// course actually created with this slug would never be reachable
		// at its own /courses/new detail page (only its /courses/new/{lang}
		// variant pages would still work). Reject it here instead.
		errMsg = "\"new\" is a reserved slug — pick a different one."
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
// editable textarea. Reuses store.VariantSource, the same lookup the API's
// GET /courses/{slug}/variants/{language} uses to round-trip a document.
// Submitting no longer writes the variant — it opens a proposal (see
// proposeVariant); there is no version hidden field anymore because the
// proposal captures its base version server-side at creation.
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
	src, _, err := h.courses.VariantSource(r.Context(), slug, lang)
	if errors.Is(err, domain.ErrNotFound) {
		if r.URL.Query().Get("new") == "1" {
			q := r.URL.Query()
			src = newVariantTemplate(slug, lang, q.Get("title"), q.Get("description"))
		} else {
			http.NotFound(w, r)
			return
		}
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.EditVariant(currentUser(r), variantEditorForm(slug, lang, src)))
}

// proposeVariant handles the editor's submit: instead of writing the
// variant directly (the pre-review-workflow behavior), it lints the
// document and opens a proposal for others to review. The one-open-
// proposal-per-user-per-variant rule turns a duplicate submit into a
// redirect to editing the existing proposal, carrying the new content is
// NOT possible across a redirect — the user lands in the existing
// proposal's editor with its current content and a notice explaining why.
//
// On validation problems or a frontmatter/URL mismatch the editor is
// re-rendered with exactly what the user submitted, never a re-fetch from
// storage, so a failed submit never discards in-progress edits.
func (h *handlers) proposeVariant(w http.ResponseWriter, r *http.Request) {
	slug, lang := r.PathValue("slug"), r.PathValue("lang")
	mdText := r.FormValue("markdown")
	title, summary := formTitleSummary(r, slug, lang)

	form := variantEditorForm(slug, lang, mdText)
	form.Title, form.Summary = r.FormValue("title"), summary

	res, problems, err := parseForEditor(mdText)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if problems != nil {
		form.Problems = problems
		h.render(w, r, views.EditVariant(currentUser(r), form))
		return
	}

	if res.Course.Course != slug || res.Course.Language != lang {
		form.ErrMsg = fmt.Sprintf("This page edits %s/%s but the document's frontmatter says %s/%s — fix the frontmatter or navigate to the matching course/language before proposing.",
			slug, lang, res.Course.Course, res.Course.Language)
		h.render(w, r, views.EditVariant(currentUser(r), form))
		return
	}
	if msg, err := renderCheckMsg(res, mdText); err != nil {
		h.serverError(w, r, err)
		return
	} else if msg != "" {
		form.ErrMsg = msg
		h.render(w, r, views.EditVariant(currentUser(r), form))
		return
	}

	user := currentUser(r) // non-nil: this route is behind h.requireUser
	p, err := h.proposals.CreateProposal(r.Context(), user.ID, slug, lang, title, summary, mdText)
	if errors.Is(err, domain.ErrDuplicateProposal) {
		if existing, ok := h.openProposalFor(r, user.ID, slug, lang); ok {
			http.Redirect(w, r, fmt.Sprintf("/proposals/%d/edit?dup=1", existing.ID), http.StatusSeeOther)
			return
		}
		form.ErrMsg = "You already have an open proposal for this course variant."
		h.render(w, r, views.EditVariant(currentUser(r), form))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
}

// openProposalFor finds the user's open proposal for a course variant —
// the row the one-open-proposal unique index just pointed at.
func (h *handlers) openProposalFor(r *http.Request, userID int64, slug, lang string) (domain.Proposal, bool) {
	mine, err := h.proposals.ListProposalsByUser(r.Context(), userID)
	if err != nil {
		return domain.Proposal{}, false
	}
	for _, p := range mine {
		if p.CourseSlug == slug && p.Language == lang && p.Status == domain.ProposalOpen {
			return p, true
		}
	}
	return domain.Proposal{}, false
}

// variantEditorForm is the views.EditorForm for proposing from a course
// page (vs. editing an existing proposal, see proposalEditorForm).
func variantEditorForm(slug, lang, markdown string) views.EditorForm {
	return views.EditorForm{
		Slug:     slug,
		Lang:     lang,
		Markdown: markdown,
		Action:   fmt.Sprintf("/courses/%s/%s/edit", slug, lang),
		Cancel:   fmt.Sprintf("/courses/%s/%s", slug, lang),
	}
}

// previewMarkdown renders whatever markdown a user currently has in the
// editor's textarea (not necessarily saved, not necessarily even valid) as
// an HTML fragment for the editor's live preview pane (issue #39). One
// fixed route (POST /preview/markdown) serves every editor — proposing
// from a course page and editing an existing proposal — since the preview
// doesn't depend on what the document targets. Gated behind h.requireUser
// same as the editors that call it.
//
// This intentionally shares nothing with the submit paths: no
// ingest.Parse/ToDomain, no store write. The preview is read-only and
// side-effect-free, so a broken or slow preview never blocks a real
// submit.
//
// The submitted markdown is a full course document, frontmatter included,
// which markdown.ToHTML doesn't know how to render as anything but stray
// text — so the leading frontmatter block is stripped first (see
// markdown.StripFrontmatter) before rendering the remainder, the same
// lesson/challenge prose a learner would actually see on the rendered
// course page.
func (h *handlers) previewMarkdown(w http.ResponseWriter, r *http.Request) {
	body := markdown.StripFrontmatter([]byte(r.FormValue("markdown")))
	html, err := markdown.ToHTML(body)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(html)); err != nil {
		h.logger.Error("write preview response", "err", err)
	}
}
