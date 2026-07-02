CREATE TABLE submissions (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    challenge_id bigint NOT NULL REFERENCES challenges ON DELETE CASCADE,
    code         text NOT NULL,
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','running','passed','failed','error')),
    score        int NOT NULL DEFAULT 0,
    tests_passed int,
    tests_total  int,
    output       text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    graded_at    timestamptz
);

CREATE INDEX submissions_best ON submissions (user_id, challenge_id, score DESC);
