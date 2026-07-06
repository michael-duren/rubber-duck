ALTER TABLE submissions
    DROP COLUMN claimed,
    DROP COLUMN audit_status,
    DROP COLUMN audit_output,
    DROP COLUMN audited_at;
