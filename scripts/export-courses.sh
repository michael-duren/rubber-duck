#!/usr/bin/env bash
# export-courses.sh — regenerate the courses/ mirror from a running server.
#
# The database is the source of truth for course content (changes go through
# the in-app proposal/review workflow); courses/*.md in this repo is a
# mirror. This script fetches every live variant's source from the public
# GET /api/v1/export endpoint (no credentials) and rewrites the top-level
# courses/*.md files to match: one file per course×language, named
# <course>-<language>.md. Top-level .md files that no longer exist on the
# server are deleted. Subdirectories (e.g. courses/os/) and everything else
# are left alone.
#
# Env:
#   DUCK_BASE_URL  server to export from (default https://duckgc.com)
#
# Run by .github/workflows/course-sync.yml on a schedule; run locally with
# `make export-courses [DUCK_URL=http://localhost:8080]`.
set -euo pipefail

base="${DUCK_BASE_URL:-https://duckgc.com}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
courses_dir="$repo_root/courses"

command -v jq >/dev/null || { echo "export-courses: jq is required" >&2; exit 1; }

export_json="$(curl -fsS "$base/api/v1/export")"

count="$(jq '.variants | length' <<<"$export_json")"
if [ "$count" -eq 0 ]; then
    # An empty export almost certainly means the wrong server or a fresh
    # database — deleting the whole mirror over it would be destructive.
    echo "export-courses: server at $base reports zero variants — refusing to empty the mirror" >&2
    exit 1
fi

# Write (or overwrite) one file per exported variant.
expected="$(mktemp)"
trap 'rm -f "$expected"' EXIT
for i in $(seq 0 $((count - 1))); do
    # -e: a missing/null field is a hard failure. Plain -r renders null as
    # the literal string "null", which passes the kebab-case guard below and
    # would auto-merge a null-null.md into the mirror on any schema drift.
    course="$(jq -er ".variants[$i].course" <<<"$export_json")"
    language="$(jq -er ".variants[$i].language" <<<"$export_json")"
    # Defense in depth: ingest already rejects non-kebab-case course/language
    # frontmatter, but these strings become filenames in a job with push
    # rights, so a name that could escape courses/ means the server-side
    # invariant broke — stop rather than write anywhere.
    if ! [[ "$course" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ && "$language" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
        echo "export-courses: refusing unsafe variant name '$course/$language' from $base" >&2
        exit 1
    fi
    file="$course-$language.md"
    # -j (not -r): raw output with no added trailing newline, so the file is
    # byte-identical to the stored source_md and re-imports diff as
    # unchanged. -e for the same schema-drift reason as above.
    jq -ej ".variants[$i].markdown" <<<"$export_json" >"$courses_dir/$file"
    echo "$file" >>"$expected"
done

# Delete top-level files whose variant left the server (never subdirs).
for f in "$courses_dir"/*.md; do
    name="$(basename "$f")"
    if ! grep -qxF "$name" "$expected"; then
        echo "export-courses: removing $name (no longer on the server)"
        rm "$f"
    fi
done

echo "export-courses: mirrored $count variant(s) from $base"
