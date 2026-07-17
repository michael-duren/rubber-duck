package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/michael-duren/rubber-duck/internal/diff"
	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/markdown"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// ProposalStore is the slice of the store the proposal pages need.
type ProposalStore interface {
	CreateProposal(ctx context.Context, proposerID int64, courseSlug, language, title, summary, markdown string) (domain.Proposal, error)
	UpdateProposalMarkdown(ctx context.Context, proposalID, proposerID int64, title, summary, markdown string) (domain.Proposal, error)
	ProposalByID(ctx context.Context, id int64) (domain.Proposal, error)
	ListProposals(ctx context.Context, status string) ([]domain.Proposal, error)
	ListProposalsByUser(ctx context.Context, userID int64) ([]domain.Proposal, error)
	ListProposalReviews(ctx context.Context, proposalID int64) ([]domain.ProposalReview, error)
	// AddReview records a verdict and reports back what the caller needs to
	// decide about publishing — the approval threshold is web config, so
	// the store deliberately doesn't publish on its own. seenRevision is
	// the proposal revision the verdict was formed against; a mismatch with
	// the current revision is ErrStaleRevision.
	AddReview(ctx context.Context, proposalID, reviewerID int64, verdict, comment string, seenRevision int) (domain.ReviewOutcome, error)
	PublishProposal(ctx context.Context, proposalID int64, course domain.Course, variant domain.Variant) (int, error)
	WithdrawProposal(ctx context.Context, proposalID, proposerID int64) error
}

// diffContext is how many unchanged lines surround each change in the
// review diff.
const diffContext = 3

// proposalsPage is the review queue (GET /proposals): open proposals by
// default, filterable by status, or the caller's own with ?mine=1. Public —
// reviewing requires an account, reading doesn't.
func (h *handlers) proposalsPage(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = domain.ProposalOpen
	}
	mine := r.URL.Query().Get("mine") == "1"

	var proposals []domain.Proposal
	var err error
	switch {
	case mine && user == nil:
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case mine:
		proposals, err = h.proposals.ListProposalsByUser(r.Context(), user.ID)
	case status == "all":
		proposals, err = h.proposals.ListProposals(r.Context(), "")
	default:
		proposals, err = h.proposals.ListProposals(r.Context(), status)
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.Proposals(user, proposals, status, mine, h.threshold))
}

// loadProposal parses the {id} path value and fetches the proposal, writing
// a 404 itself when either fails.
func (h *handlers) loadProposal(w http.ResponseWriter, r *http.Request) (domain.Proposal, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return domain.Proposal{}, false
	}
	p, err := h.proposals.ProposalByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		http.NotFound(w, r)
		return domain.Proposal{}, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return domain.Proposal{}, false
	}
	return p, true
}

// proposalPage (GET /proposals/{id}) shows the proposed change as a unified
// diff against the live variant, the review history, and — for eligible
// viewers — the approve/reject and edit/withdraw actions.
func (h *handlers) proposalPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProposal(w, r)
	if !ok {
		return
	}
	h.renderProposal(w, r, p, "")
}

// renderProposal builds everything the detail view needs. errMsg carries a
// failed action's explanation into the re-render.
func (h *handlers) renderProposal(w http.ResponseWriter, r *http.Request, p domain.Proposal, errMsg string) {
	reviews, err := h.proposals.ListProposalReviews(r.Context(), p.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// Diff against the live source; a variant that doesn't exist yet (a
	// new-course proposal) diffs against nothing, i.e. shows as all-insert.
	live, _, err := h.courses.VariantSource(r.Context(), p.CourseSlug, p.Language)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		h.serverError(w, r, err)
		return
	}
	hunks := diff.Hunks(diff.Lines(live, p.ProposedMD), diffContext)

	h.render(w, r, views.ProposalDetail(currentUser(r), p, reviews, hunks, h.threshold, errMsg))
}

// createReview (POST /proposals/{id}/reviews) records a verdict and then
// publishes when this review tipped the proposal over: an admin approval
// publishes immediately (including an admin approving their own proposal —
// the small-site bootstrap case the store's self-review check carves out),
// and a regular approval publishes once current-revision approvals reach
// the threshold. An admin rejection has already closed the proposal inside
// AddReview.
func (h *handlers) createReview(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProposal(w, r)
	if !ok {
		return
	}
	verdict := r.FormValue("verdict")
	if verdict != domain.VerdictApprove && verdict != domain.VerdictReject {
		http.Error(w, "verdict must be approve or reject", http.StatusBadRequest)
		return
	}
	// The revision the reviewer's page showed. AddReview refuses the
	// verdict if the proposer has revised since, so an approval can never
	// count toward (or worse, instantly publish) content the reviewer
	// never saw.
	seenRevision, err := strconv.Atoi(r.FormValue("revision"))
	if err != nil {
		http.Error(w, "missing or invalid revision", http.StatusBadRequest)
		return
	}
	user := currentUser(r) // non-nil: route is behind requireUser

	out, err := h.proposals.AddReview(r.Context(), p.ID, user.ID, verdict, r.FormValue("comment"), seenRevision)
	switch {
	case errors.Is(err, domain.ErrSelfReview):
		h.renderProposal(w, r, p, "You can't review your own proposal.")
		return
	case errors.Is(err, domain.ErrStaleRevision):
		h.renderProposal(w, r, p,
			"This proposal was updated while you were reviewing — your verdict wasn't recorded. Re-read the current version below and review again.")
		return
	case errors.Is(err, domain.ErrProposalClosed):
		// Someone else's review closed it between page load and submit;
		// the fresh detail page explains itself.
		http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}

	shouldPublish := !out.Closed &&
		((out.ReviewerIsAdmin && verdict == domain.VerdictApprove) ||
			out.Proposal.Approvals >= h.threshold)
	if shouldPublish {
		h.publishProposal(w, r, out.Proposal)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
}

// publishProposal parses the proposal's document and makes it live. The
// document was linted and render-checked when the proposal was
// created/updated, so a failure here means the ingest contract (or the d2
// compiler) tightened since — rare, but surfaced honestly rather than
// 500ing.
func (h *handlers) publishProposal(w http.ResponseWriter, r *http.Request, p domain.Proposal) {
	src := []byte(p.ProposedMD)
	res, err := ingest.Parse(src)
	if err != nil {
		h.renderProposal(w, r, p,
			"This proposal reached approval but its document no longer validates — the author needs to update it. "+err.Error())
		return
	}
	course, variant, err := ingest.ToDomain(res, src)
	if _, isDiagram := errors.AsType[*markdown.DiagramError](err); isDiagram {
		h.renderProposal(w, r, p,
			"This proposal reached approval but a diagram in it no longer compiles — the author needs to update it. "+err.Error())
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	_, err = h.proposals.PublishProposal(r.Context(), p.ID, course, variant)
	switch {
	case errors.Is(err, domain.ErrVersionConflict):
		// The live variant moved past the proposal's base since it was
		// authored. The review stands; the detail page's stale banner tells
		// the proposer to rebase (edit the proposal, which re-captures the
		// base version) — publishing retries on the next qualifying review.
		http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
		return
	case errors.Is(err, domain.ErrProposalClosed):
		// Lost a double-publish race; the winner already did the work.
		http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
}

// editProposalPage (GET /proposals/{id}/edit) is the proposer's editor for
// an open proposal — the same editor view as proposing from a course page,
// pointed at the update route. ?dup=1 arrives from proposeVariant when a
// second proposal for the same variant collapsed into editing this one.
func (h *handlers) editProposalPage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProposal(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if p.ProposerID != user.ID || p.Status != domain.ProposalOpen {
		http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
		return
	}
	var notice string
	if r.URL.Query().Get("dup") == "1" {
		notice = "You already had an open proposal for this course — you're editing it now."
	}
	h.render(w, r, views.EditVariant(user, proposalEditorForm(p, p.ProposedMD, notice, nil)))
}

// updateProposal (POST /proposals/{id}/edit) replaces an open proposal's
// content: lint, keep the document pointed at the same course variant, then
// bump the revision (resetting approvals) and re-capture the base version.
func (h *handlers) updateProposal(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProposal(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	mdText := r.FormValue("markdown")
	title, summary := formTitleSummary(r, p.CourseSlug, p.Language)

	form := proposalEditorForm(p, mdText, "", nil)
	form.Title, form.Summary = title, summary

	res, problems, err := parseForEditor(mdText)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if problems != nil {
		form.Problems = problems
		h.render(w, r, views.EditVariant(user, form))
		return
	}
	if res.Course.Course != p.CourseSlug || res.Course.Language != p.Language {
		form.ErrMsg = fmt.Sprintf("This proposal is for %s/%s but the document's frontmatter says %s/%s — a proposal can't change which course variant it targets.",
			p.CourseSlug, p.Language, res.Course.Course, res.Course.Language)
		h.render(w, r, views.EditVariant(user, form))
		return
	}
	if msg, err := renderCheckMsg(res, mdText); err != nil {
		h.serverError(w, r, err)
		return
	} else if msg != "" {
		form.ErrMsg = msg
		h.render(w, r, views.EditVariant(user, form))
		return
	}

	_, err = h.proposals.UpdateProposalMarkdown(r.Context(), p.ID, user.ID, title, summary, mdText)
	switch {
	case errors.Is(err, domain.ErrProposalClosed):
		form.ErrMsg = "This proposal was closed while you were editing — your changes were not saved."
		h.render(w, r, views.EditVariant(user, form))
		return
	case errors.Is(err, domain.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
}

// withdrawProposal (POST /proposals/{id}/withdraw) closes the caller's own
// open proposal.
func (h *handlers) withdrawProposal(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProposal(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	err := h.proposals.WithdrawProposal(r.Context(), p.ID, user.ID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, domain.ErrProposalClosed):
		http.Redirect(w, r, fmt.Sprintf("/proposals/%d", p.ID), http.StatusSeeOther)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/proposals?mine=1", http.StatusSeeOther)
}

// proposalEditorForm is the views.EditorForm for editing an existing
// proposal (vs. proposing from a course page, see proposeVariant).
func proposalEditorForm(p domain.Proposal, markdown, notice string, problems []views.EditProblem) views.EditorForm {
	return views.EditorForm{
		Slug:     p.CourseSlug,
		Lang:     p.Language,
		Markdown: markdown,
		Title:    p.Title,
		Summary:  p.SummaryMD,
		Action:   fmt.Sprintf("/proposals/%d/edit", p.ID),
		Cancel:   fmt.Sprintf("/proposals/%d", p.ID),
		Notice:   notice,
		Problems: problems,
	}
}

// formTitleSummary reads the shared title/summary editor fields, defaulting
// an empty title the same way the JSON API does.
func formTitleSummary(r *http.Request, slug, lang string) (title, summary string) {
	title = r.FormValue("title")
	if title == "" {
		title = fmt.Sprintf("Update %s/%s", slug, lang)
	}
	return title, r.FormValue("summary")
}

// parseForEditor runs ingest.Parse and converts validation problems into
// the editor view's display type. (nil, nil, err) is an infrastructure
// failure; (nil, problems, nil) a validation failure; (res, nil, nil)
// success.
func parseForEditor(mdText string) (*ingest.Result, []views.EditProblem, error) {
	res, err := ingest.Parse([]byte(mdText))
	if pErr, ok := errors.AsType[*ingest.ValidationError](err); ok {
		problems := make([]views.EditProblem, len(pErr.Problems))
		for i, p := range pErr.Problems {
			problems[i] = views.EditProblem{Line: p.Line, Message: p.Message}
		}
		return nil, problems, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// renderCheckMsg runs the full render pipeline publishing will run
// (ingest.ToDomain), purely as validation, so document-level problems the
// line-numbered Parse pass can't catch — today a ```d2 fence that doesn't
// compile — are rejected when the proposal is authored, not after it has
// collected approvals. ("", nil) means the document renders; a non-empty
// message is user-facing (the wrapped error names the lesson/challenge and
// d2's line:col).
func renderCheckMsg(res *ingest.Result, mdText string) (string, error) {
	_, _, err := ingest.ToDomain(res, []byte(mdText))
	if _, isDiagram := errors.AsType[*markdown.DiagramError](err); isDiagram {
		return err.Error(), nil
	}
	return "", err
}
