#!/usr/bin/env bash
set -eu

BRANCH_NAME="course-sync"

# Stage first, then diff the index: `git diff` alone ignores
# untracked files, so a drift consisting only of a brand-new
# course file would read as "up to date" and never sync.
git add -A courses
if git diff --cached --quiet -- courses; then
    echo "mirror is up to date"
    exit 0
fi

git config user.name "course-sync"
git config user.email "course-sync@users.noreply.github.com"
if git show-ref --quiet "refs/heads/$BRANCH_NAME"; then
    git checkout $BRANCH_NAME
else
    git checkout -B $BRANCH_NAME
fi

git commit -m "chore: sync courses/ mirror from production"
git push -f origin $BRANCH_NAME
# Create the PR only if no OPEN one exists for this branch (an
# open one was just updated by the force-push). The state filter
# matters: bare `gh pr view` falls back to resolving MERGED and
# CLOSED PRs for non-default branches, which after the first
# merged sync would skip the create and then fail the merge —
# bricking every later sync.
if [ "$(gh pr list --head $BRANCH_NAME --state open --json number --jq length)" = "0" ]; then
    gh pr create \
        --head $BRANCH_NAME \
        --base main \
        --title "chore: sync courses/ mirror from production" \
        --body "Automated mirror refresh from /api/v1/export. Content was reviewed in-app; CI re-verifies every document parses."
fi
