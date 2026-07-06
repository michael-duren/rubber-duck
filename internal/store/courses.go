package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// UpsertVariant atomically stores a course and one language variant.
// Re-submitting replaces the variant's lessons and challenges (cascading
// their submissions) and bumps its version. editedBy records which human
// user made the change, if any; nil means the write is agent-authored
// (e.g. the /api/v1 markdown-publish path). Returns the new version.
//
// expectedVersion implements optimistic concurrency for human-driven writes
// (the web editor, and the agent API's user-token PUT that backs `duck
// educator push`): nil means "don't check" (agent-key publishes and any
// other unversioned write path); non-nil means the write must be rejected —
// atomically, without a
// separate read-then-write race — if the variant's stored version has moved
// on since the caller loaded it. The version check happens as part of the
// variant UPSERT's own WHERE clause (UPDATE ... WHERE version = expected,
// checked via "no row returned" rather than a prior read), and on a mismatch
// this returns domain.ErrVersionConflict before committing — since the whole
// call runs in one transaction that's rolled back on any early return,
// nothing is persisted at all in that case, not even the course-level
// title/description/tags upserted earlier in this same call.
func (s *Store) UpsertVariant(ctx context.Context, course domain.Course, variant domain.Variant, editedBy *int64, expectedVersion *int) (int, error) {
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

	// The WHERE clause on the DO UPDATE action only gates the DO UPDATE
	// *action* of the INSERT ... ON CONFLICT below — when there's no
	// conflicting row at all (the variant was deleted, or never existed),
	// Postgres just takes the plain INSERT branch, which isn't subject to
	// that WHERE clause. That means an expectedVersion asserting "I loaded
	// an existing row at this version" would otherwise be silently ignored
	// whenever the row is missing, letting a stale write resurrect a
	// deleted variant. Lock and check existence explicitly first, in this
	// same transaction, so a genuinely-missing row with a non-zero expected
	// version is rejected instead of silently treated as a fresh create.
	var existingVersion int
	err = tx.QueryRow(ctx, `
		SELECT version FROM course_variants
		WHERE course_id = $1 AND language = $2
		FOR UPDATE`, courseID, variant.Language).Scan(&existingVersion)
	notFound := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !notFound {
		return 0, fmt.Errorf("lock variant: %w", err)
	}
	if expectedVersion != nil && notFound && *expectedVersion != 0 {
		return 0, domain.ErrVersionConflict
	}

	// The WHERE clause on the DO UPDATE action is the atomic version check:
	// when expectedVersion is nil the clause is always true (unconditional
	// upsert, today's behavior). When non-nil, Postgres only applies the
	// update — and only then returns a row — if the stored version still
	// matches; a concurrent winning write that bumped the version makes this
	// row's condition false, so RETURNING yields nothing and Scan reports
	// pgx.ErrNoRows, which we treat as a version conflict. This never races:
	// the check and the write happen in the same statement, not a separate
	// read followed by a write. The FOR UPDATE lock taken just above makes
	// this race-free against concurrent deletes/updates within the
	// transaction, too — not just against a stale expectedVersion.
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
		WHERE $5::int IS NULL OR course_variants.version = $5::int
		RETURNING id, version`,
		courseID, variant.Language, variant.SourceMD, editedBy, expectedVersion,
	).Scan(&variantID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrVersionConflict
	}
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
