package domain

import "time"

// Proposal statuses. Open is the only state reviews act on; the other three
// are terminal and keep the proposal (and its reviews) as history.
const (
	ProposalOpen      = "open"
	ProposalPublished = "published"
	ProposalRejected  = "rejected"
	ProposalWithdrawn = "withdrawn"
)

// Proposal kinds: what the proposed document targets. A course proposal
// carries a course-variant document keyed by (CourseSlug, Language); a path
// proposal carries a learning-path document with the path slug in
// CourseSlug and an empty Language.
const (
	KindCourse = "course"
	KindPath   = "path"
)

// Proposal is one user's suggested version of a course variant or learning
// path: a complete markdown document (the same one-file contracts
// ingest.Parse / ingest.ParsePath read) plus review-workflow state.
// BaseVersion is the live version the document was authored against — 0
// when the target doesn't exist yet — and is what publishing passes as
// expectedVersion, so a proposal whose base the world has moved past
// publishes as a conflict ("needs rebase") instead of silently overwriting
// newer content. Revision bumps every time the proposer updates the
// content; reviews record the revision they saw so stale approvals stop
// counting.
type Proposal struct {
	ID               int64
	ProposerID       int64
	ProposerUsername string
	Kind             string
	CourseSlug       string
	Language         string
	Title            string
	SummaryMD        string
	ProposedMD       string
	BaseVersion      int
	Revision         int
	Status           string
	PublishedVersion *int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ClosedAt         *time.Time

	// Approvals counts current-revision approve verdicts from users other
	// than the proposer — the number the publish threshold compares against.
	Approvals int
	// LiveVersion is the variant's current version (0 if the variant does
	// not exist). BaseVersion != LiveVersion means the proposal is stale
	// and needs a rebase before it can publish.
	LiveVersion int
}

// Stale reports whether the live target has moved past the version this
// proposal was authored against.
func (p Proposal) Stale() bool { return p.BaseVersion != p.LiveVersion }

// IsPath reports whether this proposal targets a learning path rather than
// a course variant.
func (p Proposal) IsPath() bool { return p.Kind == KindPath }

// Target is the human-readable name of what the proposal changes:
// "slug/language" for a course variant, "path slug" for a learning path.
func (p Proposal) Target() string {
	if p.IsPath() {
		return "path " + p.CourseSlug
	}
	return p.CourseSlug + "/" + p.Language
}

// Review verdicts.
const (
	VerdictApprove = "approve"
	VerdictReject  = "reject"
)

// ProposalReview is one reviewer's standing verdict on a proposal. A
// reviewer has at most one row per proposal; re-reviewing replaces the
// verdict (moving UpdatedAt) but keeps CreatedAt as the first review's time.
type ProposalReview struct {
	ID               int64
	ProposalID       int64
	ReviewerID       int64
	ReviewerUsername string
	ReviewerIsAdmin  bool
	Verdict          string
	CommentMD        string
	Revision         int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Stale reports whether the review was left on an older revision of the
// proposal than the one currently under consideration.
func (r ProposalReview) Stale(proposalRevision int) bool { return r.Revision != proposalRevision }

// ReviewOutcome is what store.AddReview reports back so the caller (which
// owns the approval threshold) can decide whether to publish. Proposal is
// the post-review state, including the recount of current-revision
// approvals.
type ReviewOutcome struct {
	Proposal        Proposal
	ReviewerIsAdmin bool
	// Closed is true when this review itself closed the proposal (an admin
	// rejection).
	Closed bool
}
