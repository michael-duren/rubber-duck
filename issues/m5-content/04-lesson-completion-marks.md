# Lesson completion marks on course pages

## Context

Deferred from M1: the variant page lists lessons but shows no per-lesson
progress for a logged-in user, and the course page's "your progress" is
only on the profile.

## Work

- Store query: per (user, variant) → challenge slugs with a passing best
  submission (join submissions max score vs challenges).
- Variant page: check/percent per lesson row + final; course page: small
  progress summary per variant when logged in.

## Done when

Passing a lesson's challenges marks it complete in the lesson list.
