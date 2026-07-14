ALTER TABLE submissions DROP COLUMN variant_version;

DROP INDEX one_final_per_variant;

-- Archived rows would violate the stricter live-only invariant (and are
-- invisible to the old code anyway), so drop them before restoring it.
-- This cascades their submissions — acceptable for a down migration of a
-- feature whose whole point was to stop that.
DELETE FROM challenges WHERE archived_at IS NOT NULL;
DELETE FROM lessons WHERE archived_at IS NOT NULL;

CREATE UNIQUE INDEX one_final_per_variant
    ON challenges (variant_id) WHERE lesson_id IS NULL;

ALTER TABLE challenges DROP COLUMN archived_at;
ALTER TABLE lessons    DROP COLUMN archived_at;
