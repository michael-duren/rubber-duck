package ingest

import "strings"

// OrderingPrefixLen returns the length of a `duck pull` directory-ordering
// prefix at the start of name — "final-", or two-plus digits plus at most
// one lowercase letter and a dash ("03-", "05a-") — or 0 if there is none.
// Single leading digits ("3-way-partition") are not a prefix.
//
// The duck CLI strips exactly this to map a pulled challenge directory back
// to its API slug, and ingest rejects challenge slugs shaped like a prefix;
// sharing one predicate keeps the strip rule and the reject rule from
// drifting apart.
func OrderingPrefixLen(name string) int {
	if strings.HasPrefix(name, "final-") {
		return len("final-")
	}
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i < 2 {
		return 0
	}
	if i < len(name) && name[i] >= 'a' && name[i] <= 'z' {
		i++
	}
	if i < len(name) && name[i] == '-' {
		return i + 1
	}
	return 0
}
