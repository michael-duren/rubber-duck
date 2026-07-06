package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// UpsertVariant atomically stores a course and one language variant.
// Re-submitting replaces the variant's lessons and challenges (cascading
// their submissions) and bumps its version. editedBy records which human
// user made the change, if any; nil means the write is agent-authored
// (e.g. the /api/v1 markdown-publish path). Returns the new version.
func (s *Store) UpsertVariant(ctx context.Context, course domain.Course, variant domain.Variant, editedBy *int64) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reading, err := json.Marshal(course.ExtendedReading)
	if err != nil {
		return 0, err
	}

	var courseID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO courses (slug, title, description_md, description_html, duration_hours, extended_reading)
		VALUES ($1, $2, $3, $4, NULLIF($5::numeric, 0), $6)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title,
			description_md = EXCLUDED.description_md,
			description_html = EXCLUDED.description_html,
			duration_hours = EXCLUDED.duration_hours,
			extended_reading = EXCLUDED.extended_reading,
			updated_at = now()
		RETURNING id`,
		course.Slug, course.Title, course.DescriptionMD, course.DescriptionHTML,
		course.DurationHours, reading,
	).Scan(&courseID)
	if err != nil {
		return 0, fmt.Errorf("upsert course: %w", err)
	}

	// Tags: replace the course's tag set; tag rows themselves are shared.
	if _, err := tx.Exec(ctx, `DELETE FROM course_tags WHERE course_id = $1`, courseID); err != nil {
		return 0, err
	}
	for _, tag := range course.Tags {
		if _, err := tx.Exec(ctx, `
			WITH t AS (
				INSERT INTO tags (name) VALUES ($1)
				ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
				RETURNING id
			)
			INSERT INTO course_tags (course_id, tag_id) SELECT $2, id FROM t`,
			tag, courseID); err != nil {
			return 0, fmt.Errorf("tag %q: %w", tag, err)
		}
	}

	var variantID int64
	var version int
	err = tx.QueryRow(ctx, `
		INSERT INTO course_variants (course_id, language, source_md, edited_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (course_id, language) DO UPDATE SET
			source_md = EXCLUDED.source_md,
			version = course_variants.version + 1,
			updated_at = now(),
			edited_by = EXCLUDED.edited_by
		RETURNING id, version`,
		courseID, variant.Language, variant.SourceMD, editedBy,
	).Scan(&variantID, &version)
	if err != nil {
		return 0, fmt.Errorf("upsert variant: %w", err)
	}

	// Replace content wholesale; deleting lessons cascades their challenges.
	if _, err := tx.Exec(ctx, `DELETE FROM lessons WHERE variant_id = $1`, variantID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM challenges WHERE variant_id = $1`, variantID); err != nil {
		return 0, err
	}

	for _, l := range variant.Lessons {
		var lessonID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO lessons (variant_id, slug, title, position, content_md, content_html)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			variantID, l.Slug, l.Title, l.Position, l.ContentMD, l.ContentHTML,
		).Scan(&lessonID)
		if err != nil {
			return 0, fmt.Errorf("lesson %q: %w", l.Slug, err)
		}
		for _, c := range l.Challenges {
			if err := insertChallenge(ctx, tx, variantID, &lessonID, c); err != nil {
				return 0, err
			}
		}
	}
	if err := insertChallenge(ctx, tx, variantID, nil, variant.Final); err != nil {
		return 0, err
	}

	return version, tx.Commit(ctx)
}

func insertChallenge(ctx context.Context, tx pgx.Tx, variantID int64, lessonID *int64, c domain.Challenge) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO challenges (variant_id, lesson_id, slug, title, position, prompt_md, prompt_html, starter_code, test_code, points)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		variantID, lessonID, c.Slug, c.Title, c.Position, c.PromptMD, c.PromptHTML, c.StarterCode, c.TestCode, c.Points)
	if err != nil {
		return fmt.Errorf("challenge %q: %w", c.Slug, err)
	}
	return nil
}
