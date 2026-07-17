package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michael-duren/rubber-duck/internal/ingest"
)

// challengeDirNames names the local directories for a pulled course's
// challenges, index-aligned with the listing. The lesson number prefixes
// the slug ("03-merge") so a directory listing sorts in course order and
// lines up with the numbered lesson list on the site. When a lesson has
// several challenges they get letter suffixes in course order ("05a-min-heap",
// "05b-heapsort") so sorting holds within the lesson too. The final
// challenge gets a "final-" prefix, which sorts after any digits.
//
// Servers predating lesson_number send 0 for every challenge; falling back
// to counting lesson transitions still yields ordered (if compacted)
// numbering rather than "00-" for everything.
func challengeDirNames(challenges []challengeJSON) []string {
	perLesson := make(map[string]int)
	for _, c := range challenges {
		if c.LessonSlug != "" {
			perLesson[c.LessonSlug]++
		}
	}
	names := make([]string, len(challenges))
	within := make(map[string]int)
	derived, prevLesson := 0, ""
	for i, c := range challenges {
		if c.LessonSlug == "" {
			names[i] = "final-" + c.Slug
			continue
		}
		if c.LessonSlug != prevLesson {
			derived, prevLesson = derived+1, c.LessonSlug
		}
		n := c.LessonNumber
		if n == 0 {
			n = derived
		}
		suffix := ""
		if perLesson[c.LessonSlug] > 1 {
			suffix = string(rune('a' + within[c.LessonSlug]))
			within[c.LessonSlug]++
		}
		names[i] = fmt.Sprintf("%02d%s-%s", n, suffix, c.Slug)
	}
	return names
}

// challengeSlug recovers the API slug from a directory name by stripping
// the ordering prefix challengeDirNames adds. Directories from older pulls
// (no prefix) pass through unchanged: ingest rejects challenge slugs shaped
// like a prefix (same predicate, so the rules can't drift), except a final
// challenge named "final-…" — whose directory gets a second "final-"
// prepended, so stripping one still recovers the slug.
func challengeSlug(dir string) string {
	return dir[ingest.OrderingPrefixLen(dir):]
}

// resolveChallenge maps what the user typed — a challenge slug or a
// directory name, prefixed or not — to the challenge's directory under
// root and its API slug.
func resolveChallenge(root, arg string) (dir, slug string, err error) {
	// Shell tab completion produces "03a-merge/" and "./03a-merge"; without
	// cleaning, the trailing slash would survive into the slug (and the URL
	// it's sent in).
	arg = filepath.Clean(arg)
	if fi, statErr := os.Stat(filepath.Join(root, arg)); statErr == nil && fi.IsDir() {
		return arg, challengeSlug(arg), nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if e.IsDir() && challengeSlug(e.Name()) == arg {
			return e.Name(), arg, nil
		}
	}
	return "", "", fmt.Errorf("no challenge %q under %s (did you `duck pull` and pick a real slug?)", arg, root)
}
