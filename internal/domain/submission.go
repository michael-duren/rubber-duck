package domain

import (
	"fmt"
	"time"
)

type Submission struct {
	ID             int64
	UserID         int64
	ChallengeID    int64
	ChallengeTitle string
	Code           string
	Status         string // pending | running | passed | failed | error
	Score          int
	Output         string
	CreatedAt      time.Time

	// TestsPassed/TestsTotal are nil when the grader couldn't parse
	// per-test counts out of the run (e.g. a panic or timeout).
	TestsPassed *int
	TestsTotal  *int
}

// Terminal reports whether grading has finished.
func (s Submission) Terminal() bool {
	return s.Status == "passed" || s.Status == "failed" || s.Status == "error"
}

// TestSummary is "N/M tests" for display, or "" when counts weren't parsed.
func (s Submission) TestSummary() string {
	if s.TestsTotal == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d tests", *s.TestsPassed, *s.TestsTotal)
}

// SubmissionJob is everything the grader needs to run one submission.
type SubmissionJob struct {
	SubmissionID int64
	Language     string
	Code         string
	TestCode     string
	Points       int
}

// CourseScore is one row of a user's profile: progress in one variant.
type CourseScore struct {
	CourseSlug  string
	CourseTitle string
	Language    string
	Earned      int
	Possible    int
}
