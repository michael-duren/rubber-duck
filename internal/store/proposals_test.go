package store

import (
	"context"
	"errors"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// proposalUsers creates the recurring cast: a proposer, two regular
// reviewers, and an admin.
func proposalUsers(t *testing.T, s *Store) (proposer, rev1, rev2, admin domain.User) {
	t.Helper()
	ctx := context.Background()
	mk := func(name string) domain.User {
		u, err := s.CreateUser(ctx, name, "h")
		if err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		return u
	}
	proposer, rev1, rev2 = mk("proposer"), mk("rev1"), mk("rev2")
	admin = mk("admin")
	if err := s.PromoteUser(ctx, "admin", domain.RoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	admin.Role = domain.RoleAdmin
	return
}

func TestCreateProposal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	proposer, _, _, _ := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	// Against a variant that doesn't exist yet: base_version 0.
	p, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "New course", "", variant.SourceMD)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.BaseVersion != 0 || p.LiveVersion != 0 || p.Stale() {
		t.Errorf("new-variant proposal base=%d live=%d stale=%v, want 0/0/false", p.BaseVersion, p.LiveVersion, p.Stale())
	}
	if p.Status != domain.ProposalOpen || p.Revision != 1 || p.Approvals != 0 || p.ProposerUsername != "proposer" {
		t.Errorf("unexpected proposal %+v", p)
	}

	// Second open proposal for the same variant by the same user: rejected.
	if _, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "again", "", variant.SourceMD); !errors.Is(err, domain.ErrDuplicateProposal) {
		t.Errorf("duplicate create err = %v, want ErrDuplicateProposal", err)
	}

	// Against a live variant: base_version captures its current version.
	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	if err := s.WithdrawProposal(ctx, p.ID, proposer.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	p2, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "Edit", "sum", variant.SourceMD+"\n<!-- edit -->")
	if err != nil {
		t.Fatalf("create against live: %v", err)
	}
	if p2.BaseVersion != 1 || p2.LiveVersion != 1 {
		t.Errorf("base=%d live=%d, want 1/1", p2.BaseVersion, p2.LiveVersion)
	}
}

func TestProposalReviewsAndThresholdCounting(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	proposer, rev1, rev2, admin := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	p, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "t", "", variant.SourceMD)
	if err != nil {
		t.Fatal(err)
	}

	// Proposer may not review their own proposal (they're not an admin).
	if _, err := s.AddReview(ctx, p.ID, proposer.ID, domain.VerdictApprove, "", 1); !errors.Is(err, domain.ErrSelfReview) {
		t.Errorf("self review err = %v, want ErrSelfReview", err)
	}

	out, err := s.AddReview(ctx, p.ID, rev1.ID, domain.VerdictApprove, "lgtm", 1)
	if err != nil {
		t.Fatalf("review 1: %v", err)
	}
	if out.Proposal.Approvals != 1 || out.ReviewerIsAdmin || out.Closed {
		t.Errorf("outcome 1 = %+v, want 1 approval, non-admin, open", out)
	}

	// A reject verdict doesn't count as an approval; re-reviewing upserts.
	if _, err := s.AddReview(ctx, p.ID, rev2.ID, domain.VerdictReject, "needs work", 1); err != nil {
		t.Fatal(err)
	}
	out, err = s.AddReview(ctx, p.ID, rev2.ID, domain.VerdictApprove, "better now", 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Proposal.Approvals != 2 {
		t.Errorf("approvals after rev2 flip = %d, want 2", out.Proposal.Approvals)
	}
	reviews, err := s.ListProposalReviews(ctx, p.ID)
	if err != nil || len(reviews) != 2 {
		t.Fatalf("reviews = %+v, %v; want 2 rows", reviews, err)
	}

	// Updating the proposal bumps revision: standing approvals stop counting
	// and display as stale.
	p2, err := s.UpdateProposalMarkdown(ctx, p.ID, proposer.ID, "t", "", variant.SourceMD+"\n<!-- v2 -->")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if p2.Revision != 2 || p2.Approvals != 0 {
		t.Errorf("after update revision=%d approvals=%d, want 2/0", p2.Revision, p2.Approvals)
	}
	reviews, _ = s.ListProposalReviews(ctx, p.ID)
	for _, r := range reviews {
		if !r.Stale(p2.Revision) {
			t.Errorf("review by %s not marked stale after revision bump", r.ReviewerUsername)
		}
	}

	// A verdict formed against the pre-update revision is refused, not
	// silently counted toward content the reviewer never read.
	if _, err := s.AddReview(ctx, p.ID, rev1.ID, domain.VerdictApprove, "old lgtm", 1); !errors.Is(err, domain.ErrStaleRevision) {
		t.Errorf("stale-revision review err = %v, want ErrStaleRevision", err)
	}

	// Re-approving after the update counts again.
	out, err = s.AddReview(ctx, p.ID, rev1.ID, domain.VerdictApprove, "still lgtm", 2)
	if err != nil || out.Proposal.Approvals != 1 {
		t.Errorf("re-approve outcome = %+v, %v; want 1 approval", out, err)
	}

	// Admin rejection closes the proposal; further reviews bounce.
	out, err = s.AddReview(ctx, p.ID, admin.ID, domain.VerdictReject, "off-topic", 2)
	if err != nil {
		t.Fatalf("admin reject: %v", err)
	}
	if !out.Closed || !out.ReviewerIsAdmin || out.Proposal.Status != domain.ProposalRejected {
		t.Errorf("admin reject outcome = %+v, want closed/rejected", out)
	}
	if _, err := s.AddReview(ctx, p.ID, rev2.ID, domain.VerdictApprove, "", 2); !errors.Is(err, domain.ErrProposalClosed) {
		t.Errorf("review after close err = %v, want ErrProposalClosed", err)
	}
}

func TestAdminSelfApprove(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, _, _, admin := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	p, err := s.CreateProposal(ctx, admin.ID, course.Slug, variant.Language, "bootstrap", "", variant.SourceMD)
	if err != nil {
		t.Fatal(err)
	}

	// The bootstrap carve-out: an admin may approve their own proposal…
	out, err := s.AddReview(ctx, p.ID, admin.ID, domain.VerdictApprove, "", 1)
	if err != nil {
		t.Fatalf("admin self-approve: %v", err)
	}
	if !out.ReviewerIsAdmin {
		t.Errorf("outcome = %+v, want ReviewerIsAdmin", out)
	}
	// …but their own approval never counts toward the community threshold
	// (publishing happens via the admin path, not the count).
	if out.Proposal.Approvals != 0 {
		t.Errorf("self-approval counted toward threshold: %d", out.Proposal.Approvals)
	}

	// An admin rejecting their own proposal is still self-review.
	if _, err := s.AddReview(ctx, p.ID, admin.ID, domain.VerdictReject, "", 1); !errors.Is(err, domain.ErrSelfReview) {
		t.Errorf("admin self-reject err = %v, want ErrSelfReview", err)
	}
}

func TestPublishProposal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	proposer, _, _, _ := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	// New-course publish: base_version 0, variant created at version 1.
	p, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "new", "", variant.SourceMD)
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.PublishProposal(ctx, p.ID, course, variant)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if version != 1 {
		t.Errorf("published version = %d, want 1", version)
	}
	got, err := s.ProposalByID(ctx, p.ID)
	if err != nil || got.Status != domain.ProposalPublished || got.PublishedVersion == nil || *got.PublishedVersion != 1 {
		t.Errorf("proposal after publish = %+v, %v", got, err)
	}
	if editedBy := variantEditedBy(t, s, course.Slug, variant.Language); editedBy == nil || *editedBy != proposer.ID {
		t.Errorf("edited_by = %v, want proposer %d", editedBy, proposer.ID)
	}

	// Publishing a closed proposal reports ErrProposalClosed (double-publish
	// race loser).
	if _, err := s.PublishProposal(ctx, p.ID, course, variant); !errors.Is(err, domain.ErrProposalClosed) {
		t.Errorf("re-publish err = %v, want ErrProposalClosed", err)
	}
}

func TestPublishProposalStaleBase(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	proposer, _, _, _ := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "edit", "", variant.SourceMD+"\n<!-- proposed -->")
	if err != nil {
		t.Fatal(err)
	}

	// The live variant moves past the proposal's base.
	newer := variant
	newer.SourceMD = variant.SourceMD + "\n<!-- concurrent publish -->"
	if _, err := s.UpsertVariant(ctx, course, newer, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Stale publish is rejected atomically and the proposal stays open for
	// a rebase (an UpdateProposalMarkdown, which re-captures base_version).
	if _, err := s.PublishProposal(ctx, p.ID, course, variant); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale publish err = %v, want ErrVersionConflict", err)
	}
	got, err := s.ProposalByID(ctx, p.ID)
	if err != nil || got.Status != domain.ProposalOpen || !got.Stale() {
		t.Errorf("proposal after failed publish = %+v, %v; want open+stale", got, err)
	}
	src, gotVersion, err := s.VariantSource(ctx, course.Slug, variant.Language)
	if err != nil || gotVersion != 2 || src != newer.SourceMD {
		t.Errorf("live variant disturbed by failed publish: v%d, %v", gotVersion, err)
	}

	// Rebase (content update re-captures base) and publish succeeds.
	if _, err := s.UpdateProposalMarkdown(ctx, p.ID, proposer.ID, "edit", "", variant.SourceMD+"\n<!-- proposed v2 -->"); err != nil {
		t.Fatal(err)
	}
	rebased := variant
	rebased.SourceMD = variant.SourceMD + "\n<!-- proposed v2 -->"
	if v, err := s.PublishProposal(ctx, p.ID, course, rebased); err != nil || v != 3 {
		t.Errorf("rebased publish = %d, %v; want version 3", v, err)
	}
}

func TestProposalOwnershipAndLists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	proposer, rev1, _, _ := proposalUsers(t, s)
	course, variant := loadSeedCourse(t)

	p, err := s.CreateProposal(ctx, proposer.ID, course.Slug, variant.Language, "t", "", variant.SourceMD)
	if err != nil {
		t.Fatal(err)
	}

	// Someone else's proposal is not yours to update or withdraw.
	if _, err := s.UpdateProposalMarkdown(ctx, p.ID, rev1.ID, "x", "", "y"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign update err = %v, want ErrNotFound", err)
	}
	if err := s.WithdrawProposal(ctx, p.ID, rev1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign withdraw err = %v, want ErrNotFound", err)
	}

	if err := s.WithdrawProposal(ctx, p.ID, proposer.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.WithdrawProposal(ctx, p.ID, proposer.ID); !errors.Is(err, domain.ErrProposalClosed) {
		t.Errorf("double withdraw err = %v, want ErrProposalClosed", err)
	}
	if _, err := s.UpdateProposalMarkdown(ctx, p.ID, proposer.ID, "x", "", "y"); !errors.Is(err, domain.ErrProposalClosed) {
		t.Errorf("update closed err = %v, want ErrProposalClosed", err)
	}

	open, err := s.ListProposals(ctx, domain.ProposalOpen)
	if err != nil || len(open) != 0 {
		t.Errorf("open list = %+v, %v; want empty", open, err)
	}
	all, err := s.ListProposals(ctx, "")
	if err != nil || len(all) != 1 {
		t.Errorf("all list = %+v, %v; want 1", all, err)
	}
	mine, err := s.ListProposalsByUser(ctx, proposer.ID)
	if err != nil || len(mine) != 1 || mine[0].Status != domain.ProposalWithdrawn {
		t.Errorf("mine = %+v, %v; want 1 withdrawn", mine, err)
	}
	if theirs, err := s.ListProposalsByUser(ctx, rev1.ID); err != nil || len(theirs) != 0 {
		t.Errorf("rev1 list = %+v, %v; want empty", theirs, err)
	}
}
