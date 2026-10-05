CREATE TABLE password_recovery_tokens (
    token_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX password_recovery_tokens_user_idx ON password_recovery_tokens (user_id);
CREATE INDEX password_recovery_tokens_expires_idx ON password_recovery_tokens (expires_at);
