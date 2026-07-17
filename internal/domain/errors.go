package domain

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrUsernameTaken = errors.New("username already taken")

	// ErrVersionConflict is returned by store.UpsertVariant when a caller
	// passes a non-nil expectedVersion that no longer matches the stored
	// version — i.e. someone else's write landed first. Callers must not
	// retry blindly; the caller re-fetching and re-presenting is the only
	// safe recovery (see internal/web's saveVariant and httpapi.putVariant's
	// version_conflict response, which `duck educator push` surfaces).
	ErrVersionConflict = errors.New("variant was changed by someone else since it was loaded")

	// ErrSelfReview: proposers may not review their own proposal. The one
	// carve-out — an admin approving their own proposal to publish it —
	// is decided in store.AddReview, not by callers.
	ErrSelfReview = errors.New("cannot review your own proposal")

	// ErrProposalClosed is returned when acting on a proposal that is no
	// longer open (published, rejected, or withdrawn).
	ErrProposalClosed = errors.New("proposal is no longer open")

	// ErrDuplicateProposal: a user already has an open proposal for this
	// course variant; the fix is updating that one, not opening another.
	ErrDuplicateProposal = errors.New("you already have an open proposal for this course variant")
)
