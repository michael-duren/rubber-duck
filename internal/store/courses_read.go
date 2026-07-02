package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/mduren/getcracked/internal/domain"
)

func (s *Store) ListCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.slug, c.title, coalesce(c.duration_hours, 0),
		       coalesce(array_agg(DISTINCT t.name) FILTER (WHERE t.name IS NOT NULL), '{}'),
		       coalesce(array_agg(DISTINCT v.language) FILTER (WHERE v.language IS NOT NULL), '{}'),
		       c.updated_at
		FROM courses c
		LEFT JOIN course_tags ct ON ct.course_id = c.id
		LEFT JOIN tags t ON t.id = ct.tag_id
		LEFT JOIN course_variants v ON v.course_id = c.id
		GROUP BY c.id
		ORDER BY c.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CourseSummary
	for rows.Next() {
		var cs domain.CourseSummary
		if err := rows.Scan(&cs.Slug, &cs.Title, &cs.DurationHours, &cs.Tags, &cs.Languages, &cs.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

// CourseBySlug returns course metadata plus a summary of each variant.
func (s *Store) CourseBySlug(ctx context.Context, slug string) (domain.Course, []domain.VariantSummary, error) {
	var c domain.Course
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.slug, c.title, c.description_md, c.description_html,
		       coalesce(c.duration_hours, 0), c.extended_reading,
		       coalesce((SELECT array_agg(t.name ORDER BY t.name)
		                 FROM course_tags ct JOIN tags t ON t.id = ct.tag_id
		                 WHERE ct.course_id = c.id), '{}'),
		       c.updated_at
		FROM courses c WHERE c.slug = $1`,
		slug,
	).Scan(&c.ID, &c.Slug, &c.Title, &c.DescriptionMD, &c.DescriptionHTML,
		&c.DurationHours, &c.ExtendedReading, &c.Tags, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Course{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.Course{}, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT v.language, v.version, v.updated_at,
		       (SELECT count(*) FROM lessons l WHERE l.variant_id = v.id),
		       (SELECT count(*) FROM challenges ch WHERE ch.variant_id = v.id),
		       (SELECT coalesce(sum(ch.points), 0) FROM challenges ch WHERE ch.variant_id = v.id)
		FROM course_variants v
		WHERE v.course_id = $1
		ORDER BY v.language`,
		c.ID)
	if err != nil {
		return domain.Course{}, nil, err
	}
	defer rows.Close()

	var vs []domain.VariantSummary
	for rows.Next() {
		var v domain.VariantSummary
		if err := rows.Scan(&v.Language, &v.Version, &v.UpdatedAt, &v.Lessons, &v.Challenges, &v.TotalPoints); err != nil {
			return domain.Course{}, nil, err
		}
		vs = append(vs, v)
	}
	return c, vs, rows.Err()
}

// VariantSource returns the stored markdown so agents can round-trip it.
func (s *Store) VariantSource(ctx context.Context, slug, language string) (string, error) {
	var src string
	err := s.pool.QueryRow(ctx, `
		SELECT v.source_md
		FROM course_variants v JOIN courses c ON c.id = v.course_id
		WHERE c.slug = $1 AND v.language = $2`,
		slug, language,
	).Scan(&src)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return src, err
}

func (s *Store) DeleteCourse(ctx context.Context, slug string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM courses WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteVariant(ctx context.Context, slug, language string) error {
	ct, err := s.pool.Exec(ctx, `
		DELETE FROM course_variants v
		USING courses c
		WHERE c.id = v.course_id AND c.slug = $1 AND v.language = $2`,
		slug, language)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ListTags(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}
