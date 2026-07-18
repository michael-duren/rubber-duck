package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	// Aliased: decodeProposalBody's named "markdown" return would shadow
	// the package name.
	md "github.com/michael-duren/rubber-duck/internal/markdown"
)

// proposalJSON is the API shape of a proposal. The document itself is only
// included on single-proposal GETs, not lists. Exactly one target is set:
// course+language for kind "course", path for kind "path".
type proposalJSON struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Course      string `json:"course,omitempty"`
	Language    string `json:"language,omitempty"`
	Path        string `json:"path,omitempty"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Markdown    string `json:"markdown,omitempty"`
	BaseVersion int    `json:"base_version"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Approvals   int    `json:"approvals"`
	Stale       bool   `json:"stale"`
	URL         string `json:"url"`
}

func toProposalJSON(p domain.Proposal, includeMarkdown bool) proposalJSON {
	j := proposalJSON{
		ID:          p.ID,
		Kind:        p.Kind,
		Title:       p.Title,
		Summary:     p.SummaryMD,
		BaseVersion: p.BaseVersion,
		Revision:    p.Revision,
		Status:      p.Status,
		Approvals:   p.Approvals,
		Stale:       p.Status == domain.ProposalOpen && p.Stale(),
		URL:         fmt.Sprintf("/proposals/%d", p.ID),
	}
	if p.IsPath() {
		j.Path = p.CourseSlug
	} else {
		j.Course, j.Language = p.CourseSlug, p.Language
	}
	if includeMarkdown {
		j.Markdown = p.ProposedMD
	}
	return j
}

// decodeProposalBody reads and validates the shared create/update request
// body. The target always comes from the document's frontmatter — the
// contract everywhere in this codebase: a "path:" key makes it a
// learning-path proposal, anything else a course-variant one. A caller that
// thinks it knows the target may send course/language (or path), and a
// mismatch is rejected rather than silently resolved in the frontmatter's
// favor. Returns ok=false with the response already written on any failure.
// For kind=path, slug is the path slug and language is "".
func (h *handlers) decodeProposalBody(w http.ResponseWriter, r *http.Request) (kind, slug, language, title, summary, markdown string, ok bool) {
	var body struct {
		Course   string `json:"course"`
		Language string `json:"language"`
		Path     string `json:"path"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Markdown string `json:"markdown"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxDocumentBytes))
	if err := dec.Decode(&body); err != nil {
		// A valid-but-huge body hits MaxBytesReader's limit and would
		// otherwise surface as a baffling "must be JSON" 400.
		if _, isMax := errors.AsType[*http.MaxBytesError](err); isMax {
			writeError(w, http.StatusRequestEntityTooLarge, "document_too_large",
				fmt.Sprintf("document exceeds the %d-byte limit", maxDocumentBytes), nil)
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", `body must be JSON: {"markdown": "..."}`, nil)
		return
	}
	if body.Markdown == "" {
		writeError(w, http.StatusBadRequest, "bad_request", `missing or empty "markdown" field`, nil)
		return
	}
	src := []byte(body.Markdown)

	if ingest.IsPathDocument(src) {
		res, parsed := h.parsePathDoc(w, r, src)
		if !parsed {
			return
		}
		slug = res.Path.Path
		if body.Course != "" || body.Language != "" {
			writeError(w, http.StatusConflict, "slug_mismatch",
				fmt.Sprintf("request names course %s/%s but the document is a learning path (%s)",
					body.Course, body.Language, slug), nil)
			return
		}
		if body.Path != "" && body.Path != slug {
			writeError(w, http.StatusConflict, "slug_mismatch",
				fmt.Sprintf("request names path %s but frontmatter says %s", body.Path, slug), nil)
			return
		}
		// Same render-check-now discipline as courses: a ```d2 fence in the
		// overview that doesn't compile must fail here, not after approvals.
		if _, err := ingest.PathToDomain(res, src); err != nil {
			if _, isDiagram := errors.AsType[*md.DiagramError](err); isDiagram {
				writeError(w, http.StatusUnprocessableEntity, "invalid_path_markdown", err.Error(), nil)
				return
			}
			h.serverError(w, r, err)
			return
		}
		title = body.Title
		if title == "" {
			title = "Update path " + slug
		}
		return domain.KindPath, slug, "", title, body.Summary, body.Markdown, true
	}

	res, parsed := h.parseCourseDoc(w, r, src)
	if !parsed {
		return
	}
	slug, language = res.Course.Course, res.Course.Language
	if body.Path != "" {
		writeError(w, http.StatusConflict, "slug_mismatch",
			fmt.Sprintf("request names path %s but the document is a course variant (%s/%s)",
				body.Path, slug, language), nil)
		return
	}
	if (body.Course != "" && body.Course != slug) || (body.Language != "" && body.Language != language) {
		writeError(w, http.StatusConflict, "slug_mismatch",
			fmt.Sprintf("request names %s/%s but frontmatter says %s/%s",
				body.Course, body.Language, slug, language), nil)
		return
	}

	// Run the full render pipeline publishing will run, purely as
	// validation: a ```d2 fence that doesn't compile is the document's
	// problem (the wrapped error names the lesson/challenge and d2's
	// line:col), and it must be rejected here — not after the proposal has
	// collected approvals and publishing chokes on it.
	if _, _, err := ingest.ToDomain(res, src); err != nil {
		if _, isDiagram := errors.AsType[*md.DiagramError](err); isDiagram {
			writeError(w, http.StatusUnprocessableEntity, "invalid_course_markdown", err.Error(), nil)
			return
		}
		h.serverError(w, r, err)
		return
	}

	title = body.Title
	if title == "" {
		title = fmt.Sprintf("Update %s/%s", slug, language)
	}
	return domain.KindCourse, slug, language, title, body.Summary, body.Markdown, true
}

// parsePathDoc is parseCourseDoc's learning-path sibling, running
// ingest.ParsePath with the same 422-with-line-details error contract.
func (h *handlers) parsePathDoc(w http.ResponseWriter, r *http.Request, src []byte) (*ingest.PathResult, bool) {
	res, err := ingest.ParsePath(src)
	if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		details := make([]Detail, len(verr.Problems))
		for i, p := range verr.Problems {
			details[i] = Detail{Line: p.Line, Message: p.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "invalid_path_markdown",
			fmt.Sprintf("%d problems found", len(details)), details)
		return nil, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return nil, false
	}
	return res, true
}

func (h *handlers) createProposal(w http.ResponseWriter, r *http.Request) {
	kind, slug, language, title, summary, markdown, ok := h.decodeProposalBody(w, r)
	if !ok {
		return
	}
	user := currentUser(r) // non-nil behind requireUser

	p, err := h.proposals.CreateProposal(r.Context(), user.ID, kind, slug, language, title, summary, markdown)
	if errors.Is(err, domain.ErrDuplicateProposal) {
		writeError(w, http.StatusConflict, "duplicate_proposal",
			"you already have an open proposal for this target — update it instead (PUT /api/v1/proposals/{id})", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProposalJSON(p, false))
}

func (h *handlers) updateProposal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	// Existence first, so a nonexistent id is a cheap 404 instead of a full
	// parse+render of the body ending in 422. Proposal metadata is readable
	// by any authenticated user (see getProposal), so checking before the
	// proposer-scoped update leaks nothing.
	existing, err := h.proposals.ProposalByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	kind, slug, language, title, summary, markdown, ok := h.decodeProposalBody(w, r)
	if !ok {
		return
	}
	user := currentUser(r)

	// A proposal can't change what it targets (the web editor enforces the
	// same rule): its diff, reviews, and re-captured base_version are all
	// anchored to the target it was opened for, so a retargeted document
	// would publish somewhere the reviewers never looked. Kind is part of
	// the target: swapping a course document into a path proposal (or vice
	// versa) is the same retargeting mistake.
	if existing.Kind != kind || existing.CourseSlug != slug || existing.Language != language {
		newTarget := domain.Proposal{Kind: kind, CourseSlug: slug, Language: language}
		writeError(w, http.StatusConflict, "variant_mismatch",
			fmt.Sprintf("this proposal is for %s but the document's frontmatter says %s — a proposal can't change what it targets; propose the new document separately",
				existing.Target(), newTarget.Target()), nil)
		return
	}

	p, err := h.proposals.UpdateProposalMarkdown(r.Context(), id, user.ID, title, summary, markdown)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// Also covers "not your proposal" — the store scopes the update to
		// the proposer, deliberately indistinguishable from missing.
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	case errors.Is(err, domain.ErrProposalClosed):
		writeError(w, http.StatusConflict, "proposal_closed",
			"this proposal is no longer open — create a new one", nil)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProposalJSON(p, false))
}

// listMyProposals lists the calling user's proposals, newest first. The
// review queue (everyone's open proposals) is a web page, not an API.
func (h *handlers) listMyProposals(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	proposals, err := h.proposals.ListProposalsByUser(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	items := make([]proposalJSON, len(proposals))
	for i, p := range proposals {
		items[i] = toProposalJSON(p, false)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": items})
}

// getProposal returns one proposal including its document. Any
// authenticated user may read any proposal — proposals are public on the
// web, the token is only for identifying authors on the write paths.
func (h *handlers) getProposal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	p, err := h.proposals.ProposalByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProposalJSON(p, true))
}

func (h *handlers) withdrawProposal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	user := currentUser(r)
	err = h.proposals.WithdrawProposal(r.Context(), id, user.ID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	case errors.Is(err, domain.ErrProposalClosed):
		writeError(w, http.StatusConflict, "proposal_closed", "this proposal is already closed", nil)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
