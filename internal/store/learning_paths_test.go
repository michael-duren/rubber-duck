package store

import (
	"context"
	"errors"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// seedPathCourses publishes n minimal courses (path-0, path-1, …) for path
// membership to reference. Reuses the seed fixture's variant content — path
// tests only care about course rows existing, not what's in them.
func seedPathCourses(t *testing.T, s *Store, n int) []string {
	t.Helper()
	course, variant := loadSeedCourse(t)
	slugs := make([]string, n)
	for i := range slugs {
		c := course
		c.Slug = "path-" + string(rune('a'+i))
		c.Title = "Path Course " + string(rune('A'+i))
		if _, err := s.UpsertVariant(context.Background(), c, variant, nil, nil); err != nil {
			t.Fatalf("seed course %s: %v", c.Slug, err)
		}
		slugs[i] = c.Slug
	}
	return slugs
}

func testPath(slug string, courses []string) domain.LearningPath {
	return domain.LearningPath{
		Slug:            slug,
		Title:           "Test Track",
		DescriptionMD:   "A test track.",
		DescriptionHTML: "<p>A test track.</p>",
		OverviewMD:      "## Why\n\nBecause.",
		OverviewHTML:    "<h2>Why</h2><p>Because.</p>",
		SourceMD:        "---\npath: " + slug + "\n---\n",
		CourseSlugs:     courses,
	}
}

func TestUpsertPath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	slugs := seedPathCourses(t, s, 3)

	created, err := s.UpsertPath(ctx, testPath("go-track", slugs))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !created {
		t.Error("first upsert reported created=false")
	}

	p, courses, err := s.PathBySlug(ctx, "go-track")
	if err != nil {
		t.Fatalf("path by slug: %v", err)
	}
	if p.Title != "Test Track" || p.OverviewMD == "" || p.SourceMD == "" {
		t.Errorf("path fields not round-tripped: %+v", p)
	}
	if len(courses) != 3 {
		t.Fatalf("got %d courses, want 3", len(courses))
	}
	for i, c := range courses {
		if c.Slug != slugs[i] {
			t.Errorf("course %d = %s, want %s (track order)", i, c.Slug, slugs[i])
		}
	}

	// Re-publish with the order reversed and one course dropped: membership
	// is replaced wholesale.
	reordered := []string{slugs[2], slugs[0]}
	created, err = s.UpsertPath(ctx, testPath("go-track", reordered))
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if created {
		t.Error("second upsert reported created=true")
	}
	p, courses, err = s.PathBySlug(ctx, "go-track")
	if err != nil {
		t.Fatalf("path by slug after update: %v", err)
	}
	if len(courses) != 2 || courses[0].Slug != slugs[2] || courses[1].Slug != slugs[0] {
		t.Errorf("membership after re-publish = %v, want %v", p.CourseSlugs, reordered)
	}
}

func TestUpsertPathUnknownCourse(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	slugs := seedPathCourses(t, s, 1)

	_, err := s.UpsertPath(ctx, testPath("bad-track", append(slugs, "no-such-course")))
	var unknown *domain.UnknownCoursesError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *domain.UnknownCoursesError", err)
	}
	if len(unknown.Slugs) != 1 || unknown.Slugs[0] != "no-such-course" {
		t.Errorf("unknown slugs = %v, want [no-such-course]", unknown.Slugs)
	}
	// The whole write must have been rejected.
	if _, _, err := s.PathBySlug(ctx, "bad-track"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("path was persisted despite unknown course: err = %v", err)
	}
}

func TestListPaths(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	slugs := seedPathCourses(t, s, 2)

	if _, err := s.UpsertPath(ctx, testPath("a-track", slugs)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	paths, err := s.ListPaths(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1", len(paths))
	}
	if paths[0].Slug != "a-track" || paths[0].CourseCount != 2 || paths[0].DescriptionHTML == "" {
		t.Errorf("summary = %+v", paths[0])
	}
}

func TestDeletePath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	slugs := seedPathCourses(t, s, 1)

	if _, err := s.UpsertPath(ctx, testPath("gone-track", slugs)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DeletePath(ctx, "gone-track"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeletePath(ctx, "gone-track"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
	// Deleting a path must not touch its member courses.
	if _, _, err := s.CourseBySlug(ctx, slugs[0]); err != nil {
		t.Errorf("member course was affected by path delete: %v", err)
	}
}

func TestDeleteCourseDropsPathMembership(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	slugs := seedPathCourses(t, s, 2)

	if _, err := s.UpsertPath(ctx, testPath("shrinking-track", slugs)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DeleteCourse(ctx, slugs[0]); err != nil {
		t.Fatalf("delete course: %v", err)
	}
	_, courses, err := s.PathBySlug(ctx, "shrinking-track")
	if err != nil {
		t.Fatalf("path by slug: %v", err)
	}
	if len(courses) != 1 || courses[0].Slug != slugs[1] {
		t.Errorf("courses after member delete = %+v, want just %s", courses, slugs[1])
	}
}
