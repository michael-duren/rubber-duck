#!/usr/bin/env bash
# sync-courses.sh — push courses/*.md to a Rubber Duck server, but only the
# variants whose markdown differs from what is already live there.
#
# `make publish` re-seeds every course unconditionally, and re-publishing a
# variant deletes its lessons/challenges and cascades to submissions. This
# script diffs first and pushes only what actually changed, so an unchanged
# course is never rewritten.
#
# Auth is `duck auth login` (the same bearer token `duck educator push` uses) —
# it must be a user token, since only a user-attributed caller gets the
# optimistic-concurrency check that makes this safe against concurrent edits.
#
#   ./scripts/sync-courses.sh                    # diff+push all of courses/*.md
#   ./scripts/sync-courses.sh --dry-run          # report what would change
#   ./scripts/sync-courses.sh courses/raft-in-go-go.md
#   DUCK_BASE_URL=http://localhost:8080 ./scripts/sync-courses.sh
set -euo pipefail

DUCK="${DUCK:-duck}"
BASE_URL="${DUCK_BASE_URL:-https://duckgc.com}"
dry_run=0
files=()

while [ $# -gt 0 ]; do
	case "$1" in
	--dry-run | -n) dry_run=1 ;;
	--base)
		[ $# -ge 2 ] || { echo "sync-courses: --base needs a URL" >&2; exit 2; }
		BASE_URL=$2
		shift
		;;
	-h | --help)
		sed -n '2,17p' "$0" | cut -c3-
		exit 0
		;;
	-*)
		echo "sync-courses: unknown flag $1" >&2
		exit 2
		;;
	*) files+=("$1") ;;
	esac
	shift
done

repo_root=$(cd "$(dirname "$0")/.." && pwd)
if [ ${#files[@]} -eq 0 ]; then
	# Top-level *.md only: courses/ also holds working subdirectories (os/)
	# that are not publishable course variants.
	files=("$repo_root"/courses/*.md)
fi

if ! duck_bin=$(command -v "$DUCK"); then
	echo "sync-courses: $DUCK not on PATH (go build -o duck ./cmd/duck, or set DUCK=...)" >&2
	exit 1
fi
# Pull runs from a temp dir (see below), so a relative DUCK like ./duck would
# resolve against *that* directory and vanish. Pin it to an absolute path here.
case "$duck_bin" in
/*) DUCK=$duck_bin ;;
*) DUCK=$(cd "$(dirname "$duck_bin")" && pwd)/$(basename "$duck_bin") ;;
esac

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

# The frontmatter is the contract the server keys on; the filename is only a
# convention, so never derive slug/language from it.
frontmatter_field() {
	awk -v key="$2" '
		NR == 1 && $0 == "---" { in_fm = 1; next }
		in_fm && $0 == "---" { exit }
		in_fm && index($0, key ":") == 1 {
			sub(/^[^:]*:[ \t]*/, "")
			print
			exit
		}
	' "$1"
}

pushed=0 unchanged=0 created=0 failed=0

for src in "${files[@]}"; do
	name=$(basename "$src")
	course=$(frontmatter_field "$src" course)
	language=$(frontmatter_field "$src" language)
	if [ -z "$course" ] || [ -z "$language" ]; then
		echo "✗ $name: no course/language in frontmatter" >&2
		failed=$((failed + 1))
		continue
	fi

	# `duck educator pull` writes <course>-<language>.md plus its sidecar into
	# the *current* directory, so give each variant its own scratch dir rather
	# than letting it land in the repo.
	pulldir="$workdir/$course-$language"
	mkdir -p "$pulldir"
	target="$pulldir/$course-$language.md"

	if (cd "$pulldir" && "$DUCK" educator pull --base "$BASE_URL" "$course/$language") \
		>"$pulldir/pull.out" 2>&1; then
		if cmp -s "$src" "$target"; then
			echo "= $name: unchanged"
			unchanged=$((unchanged + 1))
			continue
		fi
		verb="update"
	elif grep -q "404" "$pulldir/pull.out"; then
		# Not on the server yet. expected_version 0 is the server's "assert this
		# does not exist" create (internal/store/courses.go UpsertVariant), so
		# a hand-written version-0 sidecar makes push do the initial publish.
		printf '{"base_url":"%s","course":"%s","language":"%s","version":0}\n' \
			"${BASE_URL%/}" "$course" "$language" >"$target.meta.json"
		verb="create"
	else
		echo "✗ $name: pull failed" >&2
		sed 's/^/    /' "$pulldir/pull.out" >&2
		failed=$((failed + 1))
		continue
	fi

	if [ "$dry_run" -eq 1 ]; then
		echo "→ $name: would $verb"
		[ "$verb" = create ] && created=$((created + 1)) || pushed=$((pushed + 1))
		continue
	fi

	# Overwrite the pulled markdown with ours, keeping the sidecar from the
	# fresh pull — its version is what push sends as expected_version, so a
	# concurrent edit between our pull and our push is caught, not clobbered.
	cp "$src" "$target"
	if "$DUCK" educator push "$target"; then
		[ "$verb" = create ] && created=$((created + 1)) || pushed=$((pushed + 1))
	else
		echo "✗ $name: push failed" >&2
		failed=$((failed + 1))
	fi
done

echo
echo "$BASE_URL: $created created, $pushed updated, $unchanged unchanged, $failed failed"
[ "$failed" -eq 0 ]
