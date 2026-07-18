package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// UpsertPath atomically stores a learning path and its ordered course
// membership. Unlike UpsertVariant there is no archive-and-revive diffing:
// nothing (submissions, scores) hangs off path membership, so it's simply
// replaced wholesale on every publish. Every referenced course slug must
// already exist in the catalog; unknown slugs reject the whole write with a
// *domain.UnknownCoursesError so the author can fix the list (or publish
// the missing course first) in one round trip. Returns whether the path was
// created (vs updated), for the API's 201/200 split.
func (s *Store) UpsertPath(ctx context.Context, p domain.LearningPath) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var missing []string
	err = tx.QueryRow(ctx, `
		SELECT coalesce(array_agg(s.slug), '{}')
		FROM unnest($1::text[]) AS s(slug)
		WHERE NOT EXISTS (SELECT 1 FROM courses c WHERE c.slug = s.slug)`,
		p.CourseSlugs,
	).Scan(&missing)
	if err != nil {
		return false, fmt.Errorf("check course slugs: %w", err)
	}
	if len(missing) > 0 {
		return false, &domain.UnknownCoursesError{Slugs: missing}
	}

	// created_at = updated_at distinguishes insert from update: both default
	// to now() on the INSERT branch, while the DO UPDATE branch moves
	// updated_at to this (later) transaction's now().
	var pathID int64
	var created bool
	err = tx.QueryRow(ctx, `
		INSERT INTO learning_paths (slug, title, description_md, description_html,
		                            overview_md, overview_html, source_md)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			description_md = EXCLUDED.description_md,
			description_html = EXCLUDED.description_html,
			overview_md = EXCLUDED.overview_md,
			overview_html = EXCLUDED.overview_html,
			source_md = EXCLUDED.source_md,
			updated_at = now()
		RETURNING id, created_at = updated_at`,
		p.Slug, p.Title, p.DescriptionMD, p.DescriptionHTML,
		p.OverviewMD, p.OverviewHTML, p.SourceMD,
	).Scan(&pathID, &created)
	if err != nil {
		return false, fmt.Errorf("upsert path: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM learning_path_courses WHERE path_id = $1`, pathID); err != nil {
		return false, err
	}
	for i, slug := range p.CourseSlugs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO learning_path_courses (path_id, course_slug, position)
			VALUES ($1, $2, $3)`,
			pathID, slug, i+1); err != nil {
			return false, fmt.Errorf("path course %q: %w", slug, err)
		}
	}

	return created, tx.Commit(ctx)
}

func (s *Store) ListPaths(ctx context.Context) ([]domain.PathSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.slug, p.title, p.description_html, count(pc.course_slug), p.updated_at
		FROM learning_paths p
		LEFT JOIN learning_path_courses pc ON pc.path_id = p.id
		GROUP BY p.id
		ORDER BY p.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PathSummary
	for rows.Next() {
		var ps domain.PathSummary
		if err := rows.Scan(&ps.Slug, &ps.Title, &ps.DescriptionHTML, &ps.CourseCount, &ps.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// PathBySlug returns the path plus a catalog-style summary of each member
// course, in track order.
func (s *Store) PathBySlug(ctx context.Context, slug string) (domain.LearningPath, []domain.CourseSummary, error) {
	var p domain.LearningPath
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, title, description_md, description_html,
		       overview_md, overview_html, source_md, updated_at
		FROM learning_paths WHERE slug = $1`,
		slug,
	).Scan(&p.ID, &p.Slug, &p.Title, &p.DescriptionMD, &p.DescriptionHTML,
		&p.OverviewMD, &p.OverviewHTML, &p.SourceMD, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LearningPath{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.LearningPath{}, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.slug, c.title, coalesce(c.duration_hours, 0),
		       coalesce(array_agg(DISTINCT t.name) FILTER (WHERE t.name IS NOT NULL), '{}'),
		       coalesce(array_agg(DISTINCT v.language) FILTER (WHERE v.language IS NOT NULL), '{}'),
		       c.updated_at
		FROM learning_path_courses pc
		JOIN courses c ON c.slug = pc.course_slug
		LEFT JOIN course_tags ct ON ct.course_id = c.id
		LEFT JOIN tags t ON t.id = ct.tag_id
		LEFT JOIN course_variants v ON v.course_id = c.id
		WHERE pc.path_id = $1
		GROUP BY c.id, pc.position
		ORDER BY pc.position`,
		p.ID)
	if err != nil {
		return domain.LearningPath{}, nil, err
	}
	defer rows.Close()

	var courses []domain.CourseSummary
	for rows.Next() {
		var cs domain.CourseSummary
		if err := rows.Scan(&cs.Slug, &cs.Title, &cs.DurationHours, &cs.Tags, &cs.Languages, &cs.UpdatedAt); err != nil {
			return domain.LearningPath{}, nil, err
		}
		courses = append(courses, cs)
		p.CourseSlugs = append(p.CourseSlugs, cs.Slug)
	}
	return p, courses, rows.Err()
}

func (s *Store) DeletePath(ctx context.Context, slug string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM learning_paths WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
