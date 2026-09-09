-- Only a hash of the browser's opaque session is stored. Account metadata and
-- both Spotify tokens are inside AES-GCM ciphertext, bound to this session hash.
CREATE TABLE spotify_connections (
    session_hash text PRIMARY KEY CHECK (length(session_hash) = 64),
    encrypted_credentials bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX spotify_connections_expiry_idx ON spotify_connections (expires_at);
