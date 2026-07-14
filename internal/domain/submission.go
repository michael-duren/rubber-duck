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

	// VariantVersion is the course_variants.version the submission was made
	// against (stamped at insert). Comparing it to the variant's current
	// version tells whether the course has been updated since — used for the
	// "completed before the course was updated" notice; scores stay valid
	// either way.
	VariantVersion int

	// Claimed marks a verdict the duck CLI reported from a local test run.
	// The server re-grades claimed submissions in the background; the
	// outcome lands in AuditStatus/AuditOutput without touching the
	// claimed Status or Score.
	Claimed     bool
	AuditStatus string // "" until the audit finishes
	AuditOutput string
}

// Audit states for claimed submissions.
const (
	AuditNone     = ""         // not a claimed submission
	AuditPending  = "pending"  // audit hasn't run yet
	AuditVerified = "verified" // server run agreed with the claim
	AuditMismatch = "mismatch" // server run disagreed with the claim
)

// AuditState summarizes where the background audit of a claimed submission
// stands. Grader infra errors count as pending, not mismatch: they say
// nothing about the claim.
func (s Submission) AuditState() string {
	switch {
	case !s.Claimed:
		return AuditNone
	case s.AuditStatus == "" || s.AuditStatus == "error":
		return AuditPending
	case s.AuditStatus == s.Status:
		return AuditVerified
	default:
		return AuditMismatch
	}
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
	// Claimed routes the run down the audit path: the verdict already
	// stands, so the result goes to the audit columns instead.
	Claimed bool
}

// CourseScore is one row of a user's profile: progress in one variant.
type CourseScore struct {
	CourseSlug  string
	CourseTitle string
	Language    string
	Earned      int
	Possible    int
}
