CREATE TABLE institutions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    email_domain TEXT NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users
    ADD COLUMN institution_id UUID REFERENCES institutions(id),
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN approved_by UUID REFERENCES users(id),
    ADD COLUMN approved_at TIMESTAMPTZ;

CREATE TABLE access_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_name TEXT NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT NOT NULL,
    requested_role TEXT NOT NULL CHECK (requested_role IN ('admin', 'researcher', 'reviewer')),
    status TEXT NOT NULL CHECK (status IN ('email_pending', 'pending', 'approved', 'rejected', 'expired')),
    verification_token_hash BYTEA,
    verification_expires_at TIMESTAMPTZ,
    institution_id UUID REFERENCES institutions(id),
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX access_requests_open_email_idx
    ON access_requests (LOWER(email))
    WHERE status IN ('email_pending', 'pending');
CREATE INDEX access_requests_status_created_idx ON access_requests (status, created_at);

CREATE TABLE password_setup_tokens (
    token_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);

CREATE INDEX password_setup_tokens_user_idx ON password_setup_tokens (user_id);
