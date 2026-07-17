-- Roles gate the course-proposal review workflow: admins can publish or
-- reject any proposal outright; regular users publish by reaching the
-- approval threshold. Everyone starts as 'user'; promotion happens via
-- `duckserver user promote`.
ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user'
    CHECK (role IN ('user', 'admin'));
