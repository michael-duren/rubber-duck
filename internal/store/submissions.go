package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/mduren/getcracked/internal/domain"
)

// maxInFlightSubmissions caps a user's concurrent pending/running
// submissions; submitCooldownSeconds blocks re-submitting the same
// challenge too quickly. Each submission spins a real Cloud Run Job
// execution, so both exist to stop a burst (or script) from queueing
// unbounded job runs.
const (
	maxInFlightSubmissions = 3
	submitCooldownSeconds  = 10
)

// SubmissionRateLimited reports whether a new submission from this user
// (for this challenge) should be rejected: too many in-flight submissions,
// or one for the same challenge too recently.
func (s *Store) SubmissionRateLimited(ctx context.Context, userID, challengeID int64) (bool, error) {
	var limited bool
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status IN ('pending', 'running')) >= $3
			OR coalesce(bool_or(challenge_id = $2 AND created_at > now() - make_interval(secs => $4)), false)
		FROM submissions WHERE user_id = $1`,
		userID, challengeID, maxInFlightSubmissions, submitCooldownSeconds,
	).Scan(&limited)
	return limited, err
}

func (s *Store) CreateSubmission(ctx context.Context, userID, challengeID int64, code string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO submissions (user_id, challenge_id, code) VALUES ($1, $2, $3) RETURNING id`,
		userID, challengeID, code,
	).Scan(&id)
	return id, err
}

// SubmissionJob loads what the grader needs; the language comes from the
// challenge's variant.
func (s *Store) SubmissionJob(ctx context.Context, id int64) (domain.SubmissionJob, error) {
	var j domain.SubmissionJob
	err := s.pool.QueryRow(ctx, `
		SELECT sub.id, v.language, sub.code, ch.test_code, ch.points
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		JOIN course_variants v ON v.id = ch.variant_id
		WHERE sub.id = $1`,
		id,
	).Scan(&j.SubmissionID, &j.Language, &j.Code, &j.TestCode, &j.Points)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SubmissionJob{}, domain.ErrNotFound
	}
	return j, err
}

func (s *Store) MarkSubmissionRunning(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE submissions SET status = 'running' WHERE id = $1`, id)
	return err
}

func (s *Store) CompleteSubmission(ctx context.Context, id int64, status, output string, score int, testsPassed, testsTotal *int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE submissions
		SET status = $2, output = $3, score = $4, tests_passed = $5, tests_total = $6, graded_at = now()
		WHERE id = $1`,
		id, status, output, score, testsPassed, testsTotal)
	return err
}

func (s *Store) PendingSubmissionIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM submissions WHERE status IN ('pending', 'running') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SubmissionForUser fetches a submission only if it belongs to the user.
func (s *Store) SubmissionForUser(ctx context.Context, id, userID int64) (domain.Submission, error) {
	var sub domain.Submission
	err := s.pool.QueryRow(ctx, `
		SELECT sub.id, sub.user_id, sub.challenge_id, ch.title, sub.code,
		       sub.status, sub.score, coalesce(sub.output, ''), sub.created_at,
		       sub.tests_passed, sub.tests_total
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		WHERE sub.id = $1 AND sub.user_id = $2`,
		id, userID,
	).Scan(&sub.ID, &sub.UserID, &sub.ChallengeID, &sub.ChallengeTitle, &sub.Code,
		&sub.Status, &sub.Score, &sub.Output, &sub.CreatedAt,
		&sub.TestsPassed, &sub.TestsTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, err
}

// UserCourseScores sums the user's best score per challenge into one row
// per course variant they have attempted.
func (s *Store) UserCourseScores(ctx context.Context, userID int64) ([]domain.CourseScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.slug, c.title, v.language,
		       coalesce(sum(best.score), 0),
		       (SELECT coalesce(sum(ch2.points), 0) FROM challenges ch2 WHERE ch2.variant_id = v.id)
		FROM (
			SELECT challenge_id, max(score) AS score
			FROM submissions WHERE user_id = $1 GROUP BY challenge_id
		) best
		JOIN challenges ch ON ch.id = best.challenge_id
		JOIN course_variants v ON v.id = ch.variant_id
		JOIN courses c ON c.id = v.course_id
		GROUP BY c.slug, c.title, v.id, v.language
		ORDER BY c.title, v.language`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CourseScore
	for rows.Next() {
		var cs domain.CourseScore
		if err := rows.Scan(&cs.CourseSlug, &cs.CourseTitle, &cs.Language, &cs.Earned, &cs.Possible); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}
