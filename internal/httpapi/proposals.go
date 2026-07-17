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
// included on single-proposal GETs, not lists.
type proposalJSON struct {
	ID          int64  `json:"id"`
	Course      string `json:"course"`
	Language    string `json:"language"`
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
		Course:      p.CourseSlug,
		Language:    p.Language,
		Title:       p.Title,
		Summary:     p.SummaryMD,
		BaseVersion: p.BaseVersion,
		Revision:    p.Revision,
		Status:      p.Status,
		Approvals:   p.Approvals,
		Stale:       p.Status == domain.ProposalOpen && p.Stale(),
		URL:         fmt.Sprintf("/proposals/%d", p.ID),
	}
	if includeMarkdown {
		j.Markdown = p.ProposedMD
	}
	return j
}

// decodeProposalBody reads and validates the shared create/update request
// body. course/language always come from the document's frontmatter — the
// contract everywhere in this codebase — but a caller that thinks it knows
// them may send them, and a mismatch is rejected rather than silently
// resolved in the frontmatter's favor. Returns ok=false with the response
// already written on any failure.
func (h *handlers) decodeProposalBody(w http.ResponseWriter, r *http.Request) (course, language, title, summary, markdown string, ok bool) {
	var body struct {
		Course   string `json:"course"`
		Language string `json:"language"`
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

	res, parsed := h.parseCourseDoc(w, r, []byte(body.Markdown))
	if !parsed {
		return
	}
	course, language = res.Course.Course, res.Course.Language
	if (body.Course != "" && body.Course != course) || (body.Language != "" && body.Language != language) {
		writeError(w, http.StatusConflict, "slug_mismatch",
			fmt.Sprintf("request names %s/%s but frontmatter says %s/%s",
				body.Course, body.Language, course, language), nil)
		return
	}

	// Run the full render pipeline publishing will run, purely as
	// validation: a ```d2 fence that doesn't compile is the document's
	// problem (the wrapped error names the lesson/challenge and d2's
	// line:col), and it must be rejected here — not after the proposal has
	// collected approvals and publishing chokes on it.
	if _, _, err := ingest.ToDomain(res, []byte(body.Markdown)); err != nil {
		if _, isDiagram := errors.AsType[*md.DiagramError](err); isDiagram {
			writeError(w, http.StatusUnprocessableEntity, "invalid_course_markdown", err.Error(), nil)
			return
		}
		h.serverError(w, r, err)
		return
	}

	title = body.Title
	if title == "" {
		title = fmt.Sprintf("Update %s/%s", course, language)
	}
	return course, language, title, body.Summary, body.Markdown, true
}

func (h *handlers) createProposal(w http.ResponseWriter, r *http.Request) {
	course, language, title, summary, markdown, ok := h.decodeProposalBody(w, r)
	if !ok {
		return
	}
	user := currentUser(r) // non-nil behind requireUser

	p, err := h.proposals.CreateProposal(r.Context(), user.ID, course, language, title, summary, markdown)
	if errors.Is(err, domain.ErrDuplicateProposal) {
		writeError(w, http.StatusConflict, "duplicate_proposal",
			"you already have an open proposal for this course variant — update it instead (PUT /api/v1/proposals/{id})", nil)
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
	course, language, title, summary, markdown, ok := h.decodeProposalBody(w, r)
	if !ok {
		return
	}
	user := currentUser(r)

	// A proposal can't change which course variant it targets (the web
	// editor enforces the same rule): its diff, reviews, and re-captured
	// base_version are all anchored to the variant it was opened for, so a
	// retargeted document would publish somewhere the reviewers never
	// looked. Proposal metadata is readable by any authenticated user (see
	// getProposal), so checking before the proposer-scoped update leaks
	// nothing.
	existing, err := h.proposals.ProposalByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such proposal", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if existing.CourseSlug != course || existing.Language != language {
		writeError(w, http.StatusConflict, "variant_mismatch",
			fmt.Sprintf("this proposal is for %s/%s but the document's frontmatter says %s/%s — a proposal can't change which course variant it targets; propose the new document separately",
				existing.CourseSlug, existing.Language, course, language), nil)
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
