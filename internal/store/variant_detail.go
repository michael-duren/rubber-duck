package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// VariantDetail loads a course and one full variant: ordered lessons with
// their challenges, plus the final challenge.
func (s *Store) VariantDetail(ctx context.Context, courseSlug, language string) (domain.Course, domain.Variant, error) {
	var c domain.Course
	var v domain.Variant
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.slug, c.title, c.description_md, c.description_html,
		       coalesce(c.duration_hours, 0), c.extended_reading,
		       coalesce((SELECT array_agg(t.name ORDER BY t.name)
		                 FROM course_tags ct JOIN tags t ON t.id = ct.tag_id
		                 WHERE ct.course_id = c.id), '{}'),
		       c.updated_at,
		       v.id, v.language, v.version, v.updated_at, u.username
		FROM courses c
		JOIN course_variants v ON v.course_id = c.id AND v.language = $2
		LEFT JOIN users u ON u.id = v.edited_by
		WHERE c.slug = $1`,
		courseSlug, language,
	).Scan(&c.ID, &c.Slug, &c.Title, &c.DescriptionMD, &c.DescriptionHTML,
		&c.DurationHours, &c.ExtendedReading, &c.Tags, &c.UpdatedAt,
		&v.ID, &v.Language, &v.Version, &v.UpdatedAt, &v.EditedByUsername)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Course{}, domain.Variant{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Course{}, domain.Variant{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, title, position, content_md, content_html
		FROM lessons WHERE variant_id = $1 ORDER BY position`, v.ID)
	if err != nil {
		return domain.Course{}, domain.Variant{}, err
	}
	defer rows.Close()

	lessonIdx := map[int64]int{} // lesson id -> index in v.Lessons
	for rows.Next() {
		var l domain.Lesson
		if err := rows.Scan(&l.ID, &l.Slug, &l.Title, &l.Position, &l.ContentMD, &l.ContentHTML); err != nil {
			return domain.Course{}, domain.Variant{}, err
		}
		lessonIdx[l.ID] = len(v.Lessons)
		v.Lessons = append(v.Lessons, l)
	}
	if err := rows.Err(); err != nil {
		return domain.Course{}, domain.Variant{}, err
	}

	crows, err := s.pool.Query(ctx, `
		SELECT id, lesson_id, slug, title, position, prompt_md, prompt_html,
		       starter_code, test_code, points
		FROM challenges WHERE variant_id = $1 ORDER BY position`, v.ID)
	if err != nil {
		return domain.Course{}, domain.Variant{}, err
	}
	defer crows.Close()

	for crows.Next() {
		var ch domain.Challenge
		var lessonID *int64
		if err := crows.Scan(&ch.ID, &lessonID, &ch.Slug, &ch.Title, &ch.Position,
			&ch.PromptMD, &ch.PromptHTML, &ch.StarterCode, &ch.TestCode, &ch.Points); err != nil {
			return domain.Course{}, domain.Variant{}, err
		}
		if lessonID == nil {
			v.Final = ch
		} else if i, ok := lessonIdx[*lessonID]; ok {
			v.Lessons[i].Challenges = append(v.Lessons[i].Challenges, ch)
		}
	}
	return c, v, crows.Err()
}
