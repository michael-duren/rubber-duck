package domain

import "time"

// CourseSummary is a catalog-level view of a course.
type CourseSummary struct {
	Slug          string
	Title         string
	DurationHours float64
	Tags          []string
	Languages     []string
	UpdatedAt     time.Time
}

// VariantSummary describes one language variant without its content.
type VariantSummary struct {
	Language    string
	Version     int
	Lessons     int
	Challenges  int
	TotalPoints int
	UpdatedAt   time.Time
}
