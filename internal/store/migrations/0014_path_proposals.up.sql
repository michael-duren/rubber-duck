-- Learning paths join the proposal workflow (they used to be published via
-- the agent API, then briefly seed-only after that API was removed).
--
-- version gives paths the same optimistic-concurrency currency courses
-- have: proposals capture it as base_version, publishing checks it, and a
-- moved-on path surfaces as "needs rebase" instead of a silent overwrite.
-- Existing rows start at 1, same as a fresh course variant.
ALTER TABLE learning_paths ADD COLUMN version int NOT NULL DEFAULT 1;

-- kind says what a proposal targets. For 'course', (course_slug, language)
-- name a course variant as before. For 'path', course_slug holds the path
-- slug and language is '' — reusing the columns keeps the one-open-
-- proposal-per-user-per-target unique index working unchanged.
ALTER TABLE proposals ADD COLUMN kind text NOT NULL DEFAULT 'course'
    CHECK (kind IN ('course', 'path'));
