-- submissions.challenge_id is a foreign key with no index of its own: the
-- only existing index (submissions_best) leads with user_id, so it can't
-- serve challenge-scoped lookups. In particular, deleting a course or
-- variant cascades challenges -> submissions, and each deleted challenge row
-- forces a sequential scan of submissions to find its children.
CREATE INDEX submissions_challenge ON submissions (challenge_id);
