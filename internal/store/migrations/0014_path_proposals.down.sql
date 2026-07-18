-- Path proposals have no meaning without the kind column; drop them (and
-- their reviews, via cascade) rather than leaving rows that would be
-- misread as course proposals.
DELETE FROM proposals WHERE kind = 'path';
ALTER TABLE proposals DROP COLUMN kind;
ALTER TABLE learning_paths DROP COLUMN version;
