-- Attribution for who last edited a course variant. Nullable: agent-authored
-- writes (the existing /api/v1 path) have no human user; the web editor
-- (issue #35) will populate this with the acting user's ID.
ALTER TABLE course_variants
    ADD COLUMN edited_by bigint REFERENCES users(id) ON DELETE SET NULL;
