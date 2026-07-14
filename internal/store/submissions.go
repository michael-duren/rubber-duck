package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// maxInFlightSubmissions caps a user's concurrent pending/running
// submissions; maxDailySubmissionsPerChallenge caps how many times a user
// can submit the same challenge in a rolling 24h window. Each submission
// spins a real Cloud Run Job execution, so both exist to stop a burst (or
// script) from queueing unbounded job runs.
const (
	maxInFlightSubmissions          = 3
	maxDailySubmissionsPerChallenge = 5
)

// SubmissionRateLimited reports whether a new submission from this user
// (for this challenge) should be rejected: too many in-flight submissions,
// or too many submissions to this challenge in the last 24 hours. Claimed
// submissions awaiting their background audit count as in-flight — the
// audit spins the same Cloud Run Job a synchronous grade would.
func (s *Store) SubmissionRateLimited(ctx context.Context, userID, challengeID int64) (bool, error) {
	var limited bool
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status IN ('pending', 'running') OR (claimed AND audited_at IS NULL)) >= $3
			OR count(*) FILTER (WHERE challenge_id = $2 AND created_at > now() - interval '24 hours') >= $4
		FROM submissions WHERE user_id = $1`,
		userID, challengeID, maxInFlightSubmissions, maxDailySubmissionsPerChallenge,
	).Scan(&limited)
	return limited, err
}

// CreateSubmission stamps the challenge's current variant version onto the
// submission (via the SELECT feeding the INSERT), so later reads can tell
// whether the submission predates a course update.
func (s *Store) CreateSubmission(ctx context.Context, userID, challengeID int64, code string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO submissions (user_id, challenge_id, code, variant_version)
		SELECT $1, ch.id, $3, v.version
		FROM challenges ch JOIN course_variants v ON v.id = ch.variant_id
		WHERE ch.id = $2
		RETURNING id`,
		userID, challengeID, code,
	).Scan(&id)
	return id, err
}

// CreateClaimedSubmission stores a submission whose verdict the CLI already
// established locally: it lands graded, with the claimed status and score,
// and waits only for its background audit. Like CreateSubmission it stamps
// the challenge's current variant version.
func (s *Store) CreateClaimedSubmission(ctx context.Context, userID, challengeID int64, code, status, output string, score int, testsPassed, testsTotal *int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO submissions (user_id, challenge_id, code, status, output, score, tests_passed, tests_total, claimed, graded_at, variant_version)
		SELECT $1, ch.id, $3, $4, $5, $6, $7, $8, true, now(), v.version
		FROM challenges ch JOIN course_variants v ON v.id = ch.variant_id
		WHERE ch.id = $2
		RETURNING id`,
		userID, challengeID, code, status, output, score, testsPassed, testsTotal,
	).Scan(&id)
	return id, err
}

// AuditSubmission records the server-side re-grade of a claimed submission.
// It deliberately leaves status/score alone: audits inform, never revoke.
func (s *Store) AuditSubmission(ctx context.Context, id int64, status, output string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE submissions
		SET audit_status = $2, audit_output = $3, audited_at = now()
		WHERE id = $1`,
		id, status, output)
	return err
}

// SubmissionJob loads what the grader needs; the language comes from the
// challenge's variant.
func (s *Store) SubmissionJob(ctx context.Context, id int64) (domain.SubmissionJob, error) {
	var j domain.SubmissionJob
	err := s.pool.QueryRow(ctx, `
		SELECT sub.id, v.language, sub.code, ch.test_code, ch.points, sub.claimed
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		JOIN course_variants v ON v.id = ch.variant_id
		WHERE sub.id = $1`,
		id,
	).Scan(&j.SubmissionID, &j.Language, &j.Code, &j.TestCode, &j.Points, &j.Claimed)
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

// PendingSubmissionIDs returns work the pool should (re-)run: submissions
// that never finished grading, plus claimed submissions whose background
// audit hasn't landed (an audit that hit a grader infra error stays
// unaudited, so it retries on the next startup).
func (s *Store) PendingSubmissionIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM submissions
		WHERE status IN ('pending', 'running') OR (claimed AND audited_at IS NULL)
		ORDER BY id`)
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
		       sub.tests_passed, sub.tests_total,
		       sub.claimed, coalesce(sub.audit_status, ''), coalesce(sub.audit_output, ''),
		       sub.variant_version
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		WHERE sub.id = $1 AND sub.user_id = $2`,
		id, userID,
	).Scan(&sub.ID, &sub.UserID, &sub.ChallengeID, &sub.ChallengeTitle, &sub.Code,
		&sub.Status, &sub.Score, &sub.Output, &sub.CreatedAt,
		&sub.TestsPassed, &sub.TestsTotal,
		&sub.Claimed, &sub.AuditStatus, &sub.AuditOutput, &sub.VariantVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, err
}

// CompletedChallenges returns the set of challenge IDs in one variant that
// the user has a passing submission for, for lesson-completion marks.
func (s *Store) CompletedChallenges(ctx context.Context, userID, variantID int64) (map[int64]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT sub.challenge_id
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		WHERE sub.user_id = $1 AND ch.variant_id = $2 AND ch.archived_at IS NULL
		  AND sub.status = 'passed'`,
		userID, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	completed := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		completed[id] = true
	}
	return completed, rows.Err()
}

// UserCourseScores sums the user's best score per challenge into one row
// per course variant they have attempted. Earned deliberately includes
// scores on since-archived challenges — points, once earned, stay valid
// across course updates — while Possible counts only live challenges, so
// the two can briefly disagree after a course removes challenges someone
// had completed.
func (s *Store) UserCourseScores(ctx context.Context, userID int64) ([]domain.CourseScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.slug, c.title, v.language,
		       coalesce(sum(best.score), 0),
		       (SELECT coalesce(sum(ch2.points), 0) FROM challenges ch2
		        WHERE ch2.variant_id = v.id AND ch2.archived_at IS NULL)
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

// LatestSubmissionCodesByVariant returns the most recent submission code for
// each challenge in the variant that the user has attempted.
func (s *Store) LatestSubmissionCodesByVariant(ctx context.Context, userID, variantID int64) (map[int64]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (sub.challenge_id) sub.challenge_id, sub.code
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		WHERE sub.user_id = $1 AND ch.variant_id = $2
		ORDER BY sub.challenge_id, sub.created_at DESC`,
		userID, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]string)
	for rows.Next() {
		var challengeID int64
		var code string
		if err := rows.Scan(&challengeID, &code); err != nil {
			return nil, err
		}
		result[challengeID] = code
	}
	return result, rows.Err()
}

// SubmissionsForChallenge returns all submissions for a challenge by a user,
// ordered newest-first.
func (s *Store) SubmissionsForChallenge(ctx context.Context, userID, challengeID int64) ([]domain.Submission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sub.id, sub.user_id, sub.challenge_id, ch.title, sub.code,
		       sub.status, sub.score, coalesce(sub.output, ''), sub.created_at,
		       sub.tests_passed, sub.tests_total,
		       sub.claimed, coalesce(sub.audit_status, ''), coalesce(sub.audit_output, ''),
		       sub.variant_version
		FROM submissions sub
		JOIN challenges ch ON ch.id = sub.challenge_id
		WHERE sub.user_id = $1 AND sub.challenge_id = $2
		ORDER BY sub.created_at DESC`,
		userID, challengeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Submission
	for rows.Next() {
		var sub domain.Submission
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.ChallengeID, &sub.ChallengeTitle, &sub.Code,
			&sub.Status, &sub.Score, &sub.Output, &sub.CreatedAt,
			&sub.TestsPassed, &sub.TestsTotal,
			&sub.Claimed, &sub.AuditStatus, &sub.AuditOutput, &sub.VariantVersion); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}
