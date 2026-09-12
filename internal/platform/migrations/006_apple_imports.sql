-- Queued Apple Music playlist imports. The River job carries only this opaque ID.
CREATE TABLE apple_imports (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    playlist_id text NOT NULL,
    river_job_id bigint NOT NULL UNIQUE,
    error_code text NOT NULL DEFAULT '',
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX apple_imports_session_idx ON apple_imports(session_hash, created_at DESC);
