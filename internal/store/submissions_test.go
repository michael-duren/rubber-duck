package store

import (
	"context"
	"os"
	"testing"

	"github.com/mduren/getcracked/internal/domain"
	"github.com/mduren/getcracked/internal/ingest"
)

// seedChallenges publishes the seed course and returns its variant ID and
// challenge IDs.
func seedChallenges(t *testing.T, s *Store) (variantID int64, challengeIDs []int64) {
	t.Helper()
	src, err := os.ReadFile("../../seed/intro-to-go.md")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ingest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertVariant(context.Background(), course, variant); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, v, err := s.VariantDetail(context.Background(), course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("variant detail: %v", err)
	}
	var ids []int64
	for _, l := range v.Lessons {
		for _, c := range l.Challenges {
			ids = append(ids, c.ID)
		}
	}
	return v.ID, append(ids, v.Final.ID)
}

func TestSubmissionRateLimited(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, ids := seedChallenges(t, s)
	if len(ids) < 3 {
		t.Fatalf("need at least 3 challenges, got %d", len(ids))
	}
	first, second, third := ids[0], ids[1], ids[2]

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	limited, err := s.SubmissionRateLimited(ctx, u.ID, first)
	if err != nil || limited {
		t.Fatalf("fresh user: limited = %v, %v; want false", limited, err)
	}

	// Daily quota: 5 submissions to the SAME challenge, each graded
	// immediately so they don't also trip the in-flight cap, exhausts the
	// quota...
	var firstSubs []int64
	for i := range maxDailySubmissionsPerChallenge {
		id, err := s.CreateSubmission(ctx, u.ID, first, "package x")
		if err != nil {
			t.Fatalf("create submission %d: %v", i, err)
		}
		if err := s.CompleteSubmission(ctx, id, "passed", "ok", 10, nil, nil); err != nil {
			t.Fatalf("complete %d: %v", i, err)
		}
		firstSubs = append(firstSubs, id)
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, first)
	if err != nil || !limited {
		t.Errorf("at daily quota: limited = %v, %v; want true", limited, err)
	}
	// ...but a DIFFERENT challenge isn't, since the quota is per-challenge.
	limited, err = s.SubmissionRateLimited(ctx, u.ID, second)
	if err != nil || limited {
		t.Errorf("different challenge: limited = %v, %v; want false", limited, err)
	}

	// Submissions older than 24h don't count against the quota.
	if _, err := s.pool.Exec(ctx,
		`UPDATE submissions SET created_at = now() - interval '25 hours' WHERE id = $1`,
		firstSubs[0],
	); err != nil {
		t.Fatalf("backdate submission: %v", err)
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, first)
	if err != nil || limited {
		t.Errorf("after one submission ages out: limited = %v, %v; want false", limited, err)
	}

	// Max in-flight: fill the cap (3) using `second` and `third`, leaving
	// `third`'s own count untouched so checking it isolates the in-flight
	// cap from the per-challenge daily quota.
	sub2, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 2: %v", err)
	}
	sub3, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 3: %v", err)
	}
	sub4, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 4: %v", err)
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, third)
	if err != nil || !limited {
		t.Errorf("at in-flight cap: limited = %v, %v; want true", limited, err)
	}

	// Grading (no longer pending/running) frees up the in-flight slot;
	// `third` was never submitted to, so no quota masks the effect.
	for _, id := range []int64{sub2, sub3, sub4} {
		if err := s.CompleteSubmission(ctx, id, "passed", "ok", 10, nil, nil); err != nil {
			t.Fatalf("complete %d: %v", id, err)
		}
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, third)
	if err != nil || limited {
		t.Errorf("after grading: limited = %v, %v; want false", limited, err)
	}
}

// Claimed submissions land already graded, count as in-flight until their
// background audit finishes, surface via PendingSubmissionIDs until audited,
// and keep their claimed verdict after an audit disagrees.
func TestClaimedSubmissionLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, ids := seedChallenges(t, s)
	first := ids[0]

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	passed, total := 3, 4
	id, err := s.CreateClaimedSubmission(ctx, u.ID, first, "package x", "passed", "--- PASS: TestA", 8, &passed, &total)
	if err != nil {
		t.Fatalf("create claimed: %v", err)
	}

	sub, err := s.SubmissionForUser(ctx, id, u.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !sub.Claimed || sub.Status != "passed" || sub.Score != 8 {
		t.Errorf("claimed submission = %+v, want claimed passed/8", sub)
	}
	if sub.AuditState() != domain.AuditPending {
		t.Errorf("audit state = %q, want pending", sub.AuditState())
	}

	// The job the pool loads knows it's an audit run.
	job, err := s.SubmissionJob(ctx, id)
	if err != nil || !job.Claimed {
		t.Errorf("job = %+v, %v; want Claimed", job, err)
	}

	// Unaudited claims are recoverable work and count as in-flight.
	pending, err := s.PendingSubmissionIDs(ctx)
	if err != nil || len(pending) != 1 || pending[0] != id {
		t.Errorf("pending = %v, %v; want [%d]", pending, err, id)
	}
	for range maxInFlightSubmissions - 1 {
		if _, err := s.CreateClaimedSubmission(ctx, u.ID, first, "package x", "passed", "", 10, nil, nil); err != nil {
			t.Fatalf("create claimed: %v", err)
		}
	}
	limited, err := s.SubmissionRateLimited(ctx, u.ID, ids[1])
	if err != nil || !limited {
		t.Errorf("unaudited claims at cap: limited = %v, %v; want true", limited, err)
	}

	// A disagreeing audit records itself without touching the verdict.
	if err := s.AuditSubmission(ctx, id, "failed", "--- FAIL: TestA"); err != nil {
		t.Fatalf("audit: %v", err)
	}
	sub, err = s.SubmissionForUser(ctx, id, u.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sub.Status != "passed" || sub.Score != 8 {
		t.Errorf("verdict after audit = %s/%d, want passed/8 (audits never revoke)", sub.Status, sub.Score)
	}
	if sub.AuditStatus != "failed" || sub.AuditState() != domain.AuditMismatch {
		t.Errorf("audit = %q state %q, want failed/mismatch", sub.AuditStatus, sub.AuditState())
	}

	pending, err = s.PendingSubmissionIDs(ctx)
	if err != nil || len(pending) != maxInFlightSubmissions-1 {
		t.Errorf("pending after audit = %v, %v; want the %d unaudited ones", pending, err, maxInFlightSubmissions-1)
	}
}

func TestCompletedChallenges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	variantID, ids := seedChallenges(t, s)
	first, second := ids[0], ids[1]

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	completed, err := s.CompletedChallenges(ctx, u.ID, variantID)
	if err != nil || len(completed) != 0 {
		t.Fatalf("fresh user: completed = %v, %v; want empty", completed, err)
	}

	sub1, err := s.CreateSubmission(ctx, u.ID, first, "package x")
	if err != nil {
		t.Fatalf("create submission 1: %v", err)
	}
	sub2, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 2: %v", err)
	}
	// A failed submission (even with partial credit) doesn't mark the
	// challenge complete — only a passing one does.
	if err := s.CompleteSubmission(ctx, sub1, "failed", "3/4", 8, nil, nil); err != nil {
		t.Fatalf("complete 1: %v", err)
	}
	if err := s.CompleteSubmission(ctx, sub2, "passed", "ok", 15, nil, nil); err != nil {
		t.Fatalf("complete 2: %v", err)
	}

	completed, err = s.CompletedChallenges(ctx, u.ID, variantID)
	if err != nil {
		t.Fatalf("completed: %v", err)
	}
	if completed[first] {
		t.Errorf("challenge with only a failed submission marked complete")
	}
	if !completed[second] {
		t.Errorf("challenge with a passing submission not marked complete")
	}
}
