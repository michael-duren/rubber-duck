CREATE TABLE user_tokens (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    name       text NOT NULL,
    token_hash bytea UNIQUE NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

CREATE INDEX user_tokens_user ON user_tokens (user_id);
