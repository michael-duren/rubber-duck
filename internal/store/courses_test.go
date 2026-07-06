package store

import (
	"context"
	"os"
	"testing"

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

	v1, err := s.UpsertVariant(ctx, course, variant, &u.ID)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if v1 != 1 {
		t.Errorf("version = %d, want 1", v1)
	}
	if editedBy := variantEditedBy(t, s, course.Slug, variant.Language); editedBy == nil || *editedBy != u.ID {
		t.Errorf("edited_by after human edit = %v, want %d", editedBy, u.ID)
	}

	v2, err := s.UpsertVariant(ctx, course, variant, nil)
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
