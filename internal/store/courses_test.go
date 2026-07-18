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

// TestUpsertVariantNewVariantExpectedVersionZero exercises the interaction
// between genuinely-new-variant creation (expectedVersion pointing at 0, no
// existing row at all — see saveVariant's version == 0 carve-out and
// UpsertVariant's own existence check) and the optimistic-concurrency guard,
// against the real store rather than a fake: before this, only the
// in-memory fakeStore in internal/web exercised that interaction, never the
// real SQL (WHERE $5::int IS NULL OR version = $5::int against a row that
// doesn't exist yet).
func TestUpsertVariantNewVariantExpectedVersionZero(t *testing.T) {
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

	expected0 := 0

	// First-ever creation: no existing row, expectedVersion = 0. Must
	// succeed — a missing row with expectedVersion 0 is the legitimate
	// "brand new" case, not a conflict.
	v1, err := s.UpsertVariant(ctx, course, variant, nil, &expected0)
	if err != nil {
		t.Fatalf("first upsert (new variant, expectedVersion=0): %v", err)
	}
	if v1 != 1 {
		t.Errorf("version after first-ever insert = %d, want 1", v1)
	}
	gotSrc, gotVersion, err := s.VariantSource(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("VariantSource: %v", err)
	}
	if gotVersion != 1 {
		t.Errorf("stored version = %d, want 1", gotVersion)
	}
	if gotSrc != variant.SourceMD {
		t.Errorf("stored source_md does not match what was upserted")
	}

	// Second call for the same course/language, still expecting version 0:
	// the row now exists at version 1, so this must be rejected as a
	// version conflict rather than silently overwriting it.
	stale := variant
	stale.SourceMD = variant.SourceMD + "\n<!-- stale second create, must be rejected -->"
	if _, err := s.UpsertVariant(ctx, course, stale, nil, &expected0); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("second upsert (expectedVersion=0 after row exists) error = %v, want domain.ErrVersionConflict", err)
	}

	// Stored data must reflect only the first, winning write.
	gotSrc, gotVersion, err = s.VariantSource(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("VariantSource: %v", err)
	}
	if gotVersion != 1 {
		t.Errorf("stored version after rejected stale create = %d, want 1", gotVersion)
	}
	if gotSrc != variant.SourceMD {
		t.Errorf("stored source_md was clobbered by the rejected stale create")
	}
}

// loadSeedCourse parses seed/intro-to-go.md into domain values, the same
// document seedChallenges publishes.
func loadSeedCourse(t *testing.T) (domain.Course, domain.Variant) {
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
	return course, variant
}

// challengeIDsBySlug maps every live challenge (final included) to its row ID.
func challengeIDsBySlug(t *testing.T, s *Store, slug, language string) map[string]int64 {
	t.Helper()
	_, v, err := s.VariantDetail(context.Background(), slug, language)
	if err != nil {
		t.Fatalf("variant detail: %v", err)
	}
	ids := map[string]int64{v.Final.Slug: v.Final.ID}
	for _, l := range v.Lessons {
		for _, c := range l.Challenges {
			ids[c.Slug] = c.ID
		}
	}
	return ids
}

// TestUpsertVariantPreservesSubmissions is the core of the
// no-data-loss-on-republish contract: re-publishing a variant updates
// content rows in place by slug, so challenge IDs — and the submissions
// hanging off them — survive, and each submission keeps the variant version
// it was made against.
func TestUpsertVariantPreservesSubmissions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	course, variant := loadSeedCourse(t)

	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	before := challengeIDsBySlug(t, s, course.Slug, variant.Language)

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target := variant.Lessons[0].Challenges[0]
	subID, err := s.CreateSubmission(ctx, u.ID, before[target.Slug], "package x")
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	if err := s.CompleteSubmission(ctx, subID, "passed", "ok", target.Points, nil, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	sub, err := s.SubmissionForUser(ctx, subID, u.ID)
	if err != nil {
		t.Fatalf("load submission: %v", err)
	}
	if sub.VariantVersion != 1 {
		t.Errorf("submission variant_version = %d, want 1", sub.VariantVersion)
	}

	// Re-publish with edited content but the same slugs.
	updated := variant
	updated.Lessons[0].ContentMD += "\n\nrevised"
	v2, err := s.UpsertVariant(ctx, course, updated, nil, nil)
	if err != nil {
		t.Fatalf("re-publish: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("version after re-publish = %d, want 2", v2)
	}

	after := challengeIDsBySlug(t, s, course.Slug, variant.Language)
	for slug, id := range before {
		if after[slug] != id {
			t.Errorf("challenge %q ID changed across re-publish: %d -> %d", slug, id, after[slug])
		}
	}
	subs, err := s.SubmissionsForChallenge(ctx, u.ID, before[target.Slug])
	if err != nil || len(subs) != 1 {
		t.Fatalf("submissions after re-publish = %d, %v; want the original 1", len(subs), err)
	}
	if subs[0].VariantVersion != 1 {
		t.Errorf("old submission variant_version = %d, want 1 (made against v1)", subs[0].VariantVersion)
	}

	// A fresh submission is stamped with the new version.
	newSub, err := s.CreateSubmission(ctx, u.ID, before[target.Slug], "package y")
	if err != nil {
		t.Fatalf("create post-update submission: %v", err)
	}
	sub, err = s.SubmissionForUser(ctx, newSub, u.ID)
	if err != nil {
		t.Fatalf("load post-update submission: %v", err)
	}
	if sub.VariantVersion != 2 {
		t.Errorf("post-update submission variant_version = %d, want 2", sub.VariantVersion)
	}
}

// TestUpsertVariantArchiveAndRevive covers slugs leaving and re-entering the
// document: a removed challenge is archived (hidden from reads, submissions
// and their points kept), and a slug that comes back revives the same row,
// reattaching the old submissions.
func TestUpsertVariantArchiveAndRevive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	course, variant := loadSeedCourse(t)

	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	ids := challengeIDsBySlug(t, s, course.Slug, variant.Language)

	removed := variant.Lessons[0].Challenges[0]
	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	subID, err := s.CreateSubmission(ctx, u.ID, ids[removed.Slug], "package x")
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	if err := s.CompleteSubmission(ctx, subID, "passed", "ok", removed.Points, nil, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Re-publish without that challenge.
	trimmed := variant
	trimmed.Lessons = append([]domain.Lesson(nil), variant.Lessons...)
	trimmed.Lessons[0].Challenges = variant.Lessons[0].Challenges[1:]
	if _, err := s.UpsertVariant(ctx, course, trimmed, nil, nil); err != nil {
		t.Fatalf("trimmed re-publish: %v", err)
	}

	if after := challengeIDsBySlug(t, s, course.Slug, variant.Language); after[removed.Slug] != 0 {
		t.Errorf("removed challenge %q still visible after re-publish", removed.Slug)
	}
	subs, err := s.SubmissionsForChallenge(ctx, u.ID, ids[removed.Slug])
	if err != nil || len(subs) != 1 {
		t.Fatalf("submissions on archived challenge = %d, %v; want 1 (history preserved)", len(subs), err)
	}

	// Archived challenges keep their history but accept no new submissions:
	// they're hidden from every read path, so a new submission can only be a
	// crafted or stale request.
	if _, err := s.CreateSubmission(ctx, u.ID, ids[removed.Slug], "package x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("CreateSubmission on archived challenge err = %v, want ErrNotFound", err)
	}
	if _, err := s.CreateClaimedSubmission(ctx, u.ID, ids[removed.Slug], "package x", "passed", "ok", 1, nil, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("CreateClaimedSubmission on archived challenge err = %v, want ErrNotFound", err)
	}

	// Earned points survive the archival; possible points shrink to the
	// live content.
	scores, err := s.UserCourseScores(ctx, u.ID)
	if err != nil || len(scores) != 1 {
		t.Fatalf("scores = %v, %v; want 1 row", scores, err)
	}
	if scores[0].Earned != removed.Points {
		t.Errorf("earned = %d, want %d (archived challenge's points stay valid)", scores[0].Earned, removed.Points)
	}
	if wantPossible := variant.TotalPoints() - removed.Points; scores[0].Possible != wantPossible {
		t.Errorf("possible = %d, want %d (live challenges only)", scores[0].Possible, wantPossible)
	}

	// Re-publish the full document: the slug revives its archived row.
	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("revive re-publish: %v", err)
	}
	revived := challengeIDsBySlug(t, s, course.Slug, variant.Language)
	if revived[removed.Slug] != ids[removed.Slug] {
		t.Errorf("revived challenge ID = %d, want original %d (history must reattach)", revived[removed.Slug], ids[removed.Slug])
	}
	variantID, _ := seedVariantID(t, s, course.Slug, variant.Language)
	completed, err := s.CompletedChallenges(ctx, u.ID, variantID)
	if err != nil || !completed[ids[removed.Slug]] {
		t.Errorf("revived challenge not marked complete (completed=%v, err=%v)", completed, err)
	}
}

// TestUpsertVariantFinalSlugChange replaces the final challenge's slug: the
// old final must be archived (freeing the one-live-final-per-variant slot)
// and the new one inserted, in one publish.
func TestUpsertVariantFinalSlugChange(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	course, variant := loadSeedCourse(t)

	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	renamed := variant
	renamed.Final.Slug = variant.Final.Slug + "-v2"
	if _, err := s.UpsertVariant(ctx, course, renamed, nil, nil); err != nil {
		t.Fatalf("re-publish with renamed final: %v", err)
	}

	_, v, err := s.VariantDetail(ctx, course.Slug, variant.Language)
	if err != nil {
		t.Fatalf("variant detail: %v", err)
	}
	if v.Final.Slug != renamed.Final.Slug {
		t.Errorf("final slug = %q, want %q", v.Final.Slug, renamed.Final.Slug)
	}
}

// seedVariantID resolves the variant row ID for a slug/language pair.
func seedVariantID(t *testing.T, s *Store, slug, language string) (int64, int) {
	t.Helper()
	_, v, err := s.VariantDetail(context.Background(), slug, language)
	if err != nil {
		t.Fatalf("variant detail: %v", err)
	}
	return v.ID, v.Version
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

func TestListVariantSources(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Empty database: empty export, not an error.
	if got, err := s.ListVariantSources(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty export = %+v, %v; want none", got, err)
	}

	// Two languages of one course plus a second course, inserted out of
	// order to exercise the slug-then-language ordering contract.
	course, variant := loadSeedCourse(t)
	python := variant
	python.Language = "python"
	other := course
	other.Slug = "aaa-first"
	if _, err := s.UpsertVariant(ctx, course, python, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertVariant(ctx, other, variant, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertVariant(ctx, course, variant, nil, nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListVariantSources(ctx)
	if err != nil || len(got) != 3 {
		t.Fatalf("export = %d rows, %v; want 3", len(got), err)
	}
	wantOrder := []struct{ slug, lang string }{
		{other.Slug, variant.Language},
		{course.Slug, variant.Language},
		{course.Slug, "python"},
	}
	for i, w := range wantOrder {
		if got[i].CourseSlug != w.slug || got[i].Language != w.lang {
			t.Errorf("row %d = %s/%s, want %s/%s", i, got[i].CourseSlug, got[i].Language, w.slug, w.lang)
		}
	}
	if got[0].Version != 1 || got[0].SourceMD != variant.SourceMD {
		t.Errorf("row 0 version=%d, source round-trip ok=%v", got[0].Version, got[0].SourceMD == variant.SourceMD)
	}
}
