package domain

import "time"

// Course is language-independent metadata shared by all variants.
type Course struct {
	ID              int64
	Slug            string
	Title           string
	DescriptionMD   string
	DescriptionHTML string
	DurationHours   float64
	Tags            []string
	ExtendedReading []Reading
	UpdatedAt       time.Time
}

type Reading struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Variant is one programming-language rendition of a course. It owns its
// lessons and challenges: translations may differ in granularity.
type Variant struct {
	ID       int64
	Language string
	SourceMD string
	// Version is populated on reads from course_variants.version. It is not
	// consulted on writes: store.UpsertVariant's optimistic-concurrency check
	// and the version it writes come from its own expectedVersion parameter
	// and return value, not from this field — setting it before a write has
	// no effect.
	Version int
	Lessons []Lesson
	Final   Challenge
	// EditedByUsername is the username of the user who last saved this
	// variant through the web editor. It is nil when the variant was
	// agent-authored via the /api/v1 ingest path (edited_by IS NULL) or has
	// never been edited through the web UI.
	EditedByUsername *string
	UpdatedAt        time.Time
}

type Lesson struct {
	ID          int64
	Slug        string
	Title       string
	Position    int
	ContentMD   string
	ContentHTML string
	Challenges  []Challenge
}

type Challenge struct {
	ID          int64
	Slug        string
	Title       string
	Position    int
	PromptMD    string
	PromptHTML  string
	StarterCode string
	TestCode    string
	Points      int
}

// TotalPoints is the maximum score achievable in the variant.
func (v Variant) TotalPoints() int {
	total := v.Final.Points
	for _, l := range v.Lessons {
		for _, c := range l.Challenges {
			total += c.Points
		}
	}
	return total
}

// ChallengeCount includes the final challenge.
func (v Variant) ChallengeCount() int {
	n := 1
	for _, l := range v.Lessons {
		n += len(l.Challenges)
	}
	return n
}
