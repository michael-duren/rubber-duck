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

	v1, err := s.UpsertVariant(ctx, course, variant)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if v1 != 1 {
		t.Errorf("version = %d, want 1", v1)
	}

	v2, err := s.UpsertVariant(ctx, course, variant)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if v2 != 2 {
		t.Errorf("version = %d, want 2", v2)
	}
}
