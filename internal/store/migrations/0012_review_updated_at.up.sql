-- Re-reviewing upserts the reviewer's row; before this column that upsert
-- reset created_at, destroying the first-seen time. created_at now keeps
-- the original review's timestamp and updated_at tracks the latest verdict
-- (what "newest activity first" ordering uses).
ALTER TABLE proposal_reviews
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
