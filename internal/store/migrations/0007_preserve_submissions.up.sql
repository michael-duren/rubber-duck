-- Re-publishing a course variant used to replace its lessons/challenges
-- wholesale, cascading a delete of every submission. Content rows are now
-- diffed by slug instead (see store.UpsertVariant): rows whose slug is still
-- in the document are updated in place (IDs stable, submissions untouched),
-- and rows whose slug disappeared are archived, not deleted — their
-- submissions remain as history. A slug that reappears revives its archived
-- row, reattaching that history.
ALTER TABLE lessons    ADD COLUMN archived_at timestamptz;
ALTER TABLE challenges ADD COLUMN archived_at timestamptz;

-- The one-final-challenge invariant only applies to live rows: replacing the
-- final challenge's slug archives the old row (still lesson_id IS NULL) and
-- inserts a new one.
DROP INDEX one_final_per_variant;
CREATE UNIQUE INDEX one_final_per_variant
    ON challenges (variant_id) WHERE lesson_id IS NULL AND archived_at IS NULL;

-- variant_version records which content version of the variant a submission
-- was graded against, so the UI can tell "you completed this before the
-- course was updated". Pre-existing submissions can't know their true
-- historical version, so they're backfilled with the variant's current one —
-- treating them as current avoids flagging every old submission as outdated
-- the moment this migration lands.
ALTER TABLE submissions ADD COLUMN variant_version int NOT NULL DEFAULT 0;
UPDATE submissions sub
SET variant_version = v.version
FROM challenges ch
JOIN course_variants v ON v.id = ch.variant_id
WHERE ch.id = sub.challenge_id;
