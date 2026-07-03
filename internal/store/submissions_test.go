package store

import (
	"context"
	"os"
	"testing"

	"github.com/mduren/getcracked/internal/ingest"
)

// seedChallenges publishes the seed course and returns its challenge IDs.
func seedChallenges(t *testing.T, s *Store) []int64 {
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
	return append(ids, v.Final.ID)
}

func TestSubmissionRateLimited(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ids := seedChallenges(t, s)
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

	// Cooldown: a second submission to the SAME challenge right away is
	// blocked...
	sub1, err := s.CreateSubmission(ctx, u.ID, first, "package x")
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, first)
	if err != nil || !limited {
		t.Errorf("same challenge cooldown: limited = %v, %v; want true", limited, err)
	}
	// ...but a DIFFERENT challenge isn't, since only one submission is
	// in-flight (under the max-in-flight cap of 3).
	limited, err = s.SubmissionRateLimited(ctx, u.ID, second)
	if err != nil || limited {
		t.Errorf("different challenge: limited = %v, %v; want false", limited, err)
	}

	// Max in-flight: fill the cap (3) using only `first` and `second`,
	// leaving `third` untouched so checking it isolates the in-flight cap
	// from the per-challenge cooldown.
	sub2, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 2: %v", err)
	}
	sub3, err := s.CreateSubmission(ctx, u.ID, second, "package x")
	if err != nil {
		t.Fatalf("create submission 3: %v", err)
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, third)
	if err != nil || !limited {
		t.Errorf("at in-flight cap: limited = %v, %v; want true", limited, err)
	}

	// Grading (no longer pending/running) frees up the in-flight slot;
	// `third` was never submitted to, so no cooldown masks the effect.
	for _, id := range []int64{sub1, sub2, sub3} {
		if err := s.CompleteSubmission(ctx, id, "passed", "ok", 10); err != nil {
			t.Fatalf("complete %d: %v", id, err)
		}
	}
	limited, err = s.SubmissionRateLimited(ctx, u.ID, third)
	if err != nil || limited {
		t.Errorf("after grading: limited = %v, %v; want false", limited, err)
	}
}
