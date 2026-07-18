package domain

import (
	"strings"
	"time"
)

// LearningPath is a curated, ordered track of courses — "start here, then
// this" — layered on top of the catalog. Paths reference courses by slug
// (the same durable identity agents author against everywhere else) and
// carry no learner data of their own: progress through a path is derived
// entirely from the member courses' submissions.
type LearningPath struct {
	ID              int64
	Slug            string
	Title           string
	DescriptionMD   string
	DescriptionHTML string

	// Overview is the path document's markdown body: the long-form pitch
	// rendered on the path page under the description.
	OverviewMD   string
	OverviewHTML string

	// SourceMD is the original path document, kept verbatim so the agent
	// API can round-trip it, mirroring Variant.SourceMD.
	SourceMD string

	// CourseSlugs is the track order. The store validates each slug refers
	// to an existing course at upsert time.
	CourseSlugs []string

	UpdatedAt time.Time
}

// UnknownCoursesError is returned by store.UpsertPath when a path document
// references course slugs that don't exist in the catalog. It's a
// validation failure from the author's point of view (fix the list, or
// publish the missing course first), so the agent API reports it like one.
type UnknownCoursesError struct {
	Slugs []string
}

func (e *UnknownCoursesError) Error() string {
	return "unknown course slugs: " + strings.Join(e.Slugs, ", ")
}

// PathSummary is the paths-index view of a learning path.
type PathSummary struct {
	Slug            string
	Title           string
	DescriptionHTML string
	CourseCount     int
	UpdatedAt       time.Time
}
