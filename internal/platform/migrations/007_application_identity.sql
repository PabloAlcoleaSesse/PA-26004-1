-- Application identity is separate from provider credentials. A browser
-- session maps to a durable user, while provider account IDs remain opaque.
CREATE TABLE app_users (
    id text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_sessions (
    session_hash text PRIMARY KEY CHECK (length(session_hash) = 64),
    user_id text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_sessions_user_idx ON user_sessions(user_id);
CREATE INDEX user_sessions_expiry_idx ON user_sessions(expires_at);

CREATE TABLE provider_accounts (
    user_id text NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
    provider text NOT NULL,
    account_id text NOT NULL,
    linked_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, provider),
    UNIQUE (provider, account_id)
);
