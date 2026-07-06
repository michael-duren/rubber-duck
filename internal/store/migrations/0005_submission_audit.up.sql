-- Claimed submissions: the duck CLI ran the tests locally and reported the
-- verdict; the server accepts it immediately and re-grades in the background
-- as an audit. Audit results are informational (a badge), never punitive —
-- they must not rewrite status/score.
ALTER TABLE submissions
    ADD COLUMN claimed boolean NOT NULL DEFAULT false,
    ADD COLUMN audit_status text
        CHECK (audit_status IN ('passed', 'failed', 'error')),
    ADD COLUMN audit_output text,
    ADD COLUMN audited_at timestamptz;
