package ingest

import (
	"fmt"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/markdown"
)

// ToDomain renders HTML for every section and assembles the domain course
// and variant ready for storage. src is the original document, kept verbatim
// as the variant's source of truth.
func ToDomain(res *Result, src []byte) (domain.Course, domain.Variant, error) {
	descHTML, err := markdown.ToHTML([]byte(res.Course.Description))
	if err != nil {
		return domain.Course{}, domain.Variant{}, err
	}

	course := domain.Course{
		Slug:            res.Course.Course,
		Title:           res.Course.Title,
		DescriptionMD:   res.Course.Description,
		DescriptionHTML: descHTML,
		DurationHours:   res.Course.DurationHours,
		Tags:            res.Course.Tags,
	}
	for _, r := range res.Course.ExtendedReading {
		course.ExtendedReading = append(course.ExtendedReading, domain.Reading(r))
	}

	variant := domain.Variant{
		Language: res.Course.Language,
		SourceMD: string(src),
	}
	for i, l := range res.Lessons {
		lesson := domain.Lesson{
			Slug:      l.Slug,
			Title:     l.Title,
			Position:  i + 1,
			ContentMD: l.ContentMD,
		}
		if lesson.ContentHTML, err = markdown.ToHTML([]byte(l.ContentMD)); err != nil {
			return domain.Course{}, domain.Variant{}, fmt.Errorf("lesson %s: %w", l.Slug, err)
		}
		for j, c := range l.Challenges {
			dc, err := challengeToDomain(c, j+1)
			if err != nil {
				return domain.Course{}, domain.Variant{}, err
			}
			lesson.Challenges = append(lesson.Challenges, dc)
		}
		variant.Lessons = append(variant.Lessons, lesson)
	}
	if variant.Final, err = challengeToDomain(res.Final, 1); err != nil {
		return domain.Course{}, domain.Variant{}, err
	}
	return course, variant, nil
}

func challengeToDomain(c ParsedChallenge, position int) (domain.Challenge, error) {
	html, err := markdown.ToHTML([]byte(c.PromptMD))
	if err != nil {
		return domain.Challenge{}, fmt.Errorf("challenge %s: %w", c.Slug, err)
	}
	return domain.Challenge{
		Slug:        c.Slug,
		Title:       c.Title,
		Position:    position,
		PromptMD:    c.PromptMD,
		PromptHTML:  html,
		StarterCode: c.StarterCode,
		TestCode:    c.TestCode,
		Points:      c.Points,
	}, nil
}
