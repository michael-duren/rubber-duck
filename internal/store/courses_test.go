package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/ingest"
)

func TestUpsertVariant(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

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

	u, err := s.CreateUser(ctx, "editor", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	v1, err := s.UpsertVariant(ctx, course, variant, &u.ID, nil)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if v1 != 1 {
		t.Errorf("version = %d, want 1", v1)
	}
	if editedBy := variantEditedBy(t, s, course.Slug, variant.Language); editedBy == nil || *editedBy != u.ID {
		t.Errorf("edited_by after human edit = %v, want %d", editedBy, u.ID)
	}

	v2, err := s.UpsertVariant(ctx, course, variant, nil, nil)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if v2 != 2 {
		t.Errorf("version = %d, want 2", v2)
	}
	if editedBy := variantEditedBy(t, s, course.Slug, variant.Language); editedBy != nil {
		t.Errorf("edited_by after agent (nil) edit = %v, want nil", editedBy)
	}
}

// TestUpsertVariantVersionConflict exercises the optimistic-concurrency path
// (issue #36): a save whose expectedVersion no longer matches the stored
// version must be rejected atomically — no read-then-write race — and must
// not touch the stored data at all, leaving only the winning write in place.
func TestUpsertVariantVersionConflict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

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

	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Winning write: correctly expects version 1 (the current stored
	// version), so it's applied and bumps the version to 2.
	winning := variant
	winning.SourceMD = variant.SourceMD + "\n<!-- winning edit -->"
	expected1 := 1
	v2, err := s.UpsertVariant(ctx, course, winning, nil, &expected1)
	if err != nil {
		t.Fatalf("winning upsert: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("version after winning upsert = %d, want 2", v2)
	}

	// Stale write: still expects version 1, but the actual stored version
	// has already moved to 2 (the winning write above) — must be rejected,
	// not silently applied on top.
	stale := variant
	stale.SourceMD = variant.SourceMD + "\n<!-- stale edit, must be rejected -->"
	if _, err := s.UpsertVariant(ctx, course, stale, nil, &expected1); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale upsert error = %v, want domain.ErrVersionConflict", err)
	}

	// Stored data must reflect only the winning write.
	gotSrc, gotVersion, err := s.VariantSource(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("VariantSource: %v", err)
	}
	if gotVersion != 2 {
		t.Errorf("stored version = %d, want 2 (stale write must not bump it further)", gotVersion)
	}
	if gotSrc != winning.SourceMD {
		t.Errorf("stored source_md was clobbered by the rejected stale write")
	}
}

// TestVariantSubmissionCount covers issue #37: the web editor warns before a
// save that would cascade-delete submissions, using this count to name the
// exact impact.
func TestVariantSubmissionCount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, ids := seedChallenges(t, s)
	first, second := ids[0], ids[1]

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

	// A freshly-seeded variant has no submissions yet.
	count, err := s.VariantSubmissionCount(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("VariantSubmissionCount: %v", err)
	}
	if count != 0 {
		t.Errorf("count before any submissions = %d, want 0", count)
	}

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := s.CreateSubmission(ctx, u.ID, first, "package x"); err != nil {
		t.Fatalf("create submission 1: %v", err)
	}
	if _, err := s.CreateSubmission(ctx, u.ID, second, "package x"); err != nil {
		t.Fatalf("create submission 2: %v", err)
	}

	count, err = s.VariantSubmissionCount(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("VariantSubmissionCount: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (one per submitted challenge, regardless of user)", count)
	}

	// A nonexistent variant reports 0 rather than erroring.
	if noneCount, err := s.VariantSubmissionCount(ctx, "nope", "go"); err != nil || noneCount != 0 {
		t.Errorf("count for missing variant = %d, %v; want 0, nil", noneCount, err)
	}
}

// variantEditedBy reads the edited_by column directly; it's attribution
// metadata, not part of any domain read path yet.
func variantEditedBy(t *testing.T, s *Store, courseSlug, language string) *int64 {
	t.Helper()
	var editedBy *int64
	err := s.pool.QueryRow(context.Background(), `
		SELECT cv.edited_by
		FROM course_variants cv
		JOIN courses c ON c.id = cv.course_id
		WHERE c.slug = $1 AND cv.language = $2`,
		courseSlug, language,
	).Scan(&editedBy)
	if err != nil {
		t.Fatalf("query edited_by: %v", err)
	}
	return editedBy
}
