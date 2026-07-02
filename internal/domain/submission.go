package domain

import "time"

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
}

// Terminal reports whether grading has finished.
func (s Submission) Terminal() bool {
	return s.Status == "passed" || s.Status == "failed" || s.Status == "error"
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
