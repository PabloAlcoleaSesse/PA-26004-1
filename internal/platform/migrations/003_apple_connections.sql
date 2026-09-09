CREATE TABLE apple_connections (
    session_hash text PRIMARY KEY CHECK (length(session_hash) = 64),
    encrypted_credentials bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX apple_connections_expiry_idx ON apple_connections (expires_at);
