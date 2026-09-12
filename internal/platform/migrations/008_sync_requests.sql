-- One-way, opt-in synchronization requests. A River job carries only the
-- opaque request ID; provider credentials remain in encrypted connection rows.
CREATE TABLE sync_requests (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    source_provider text NOT NULL,
    source_playlist_id text NOT NULL,
    destination_provider text NOT NULL,
    destination_playlist_id text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','completed','failed','cancelled')),
    error_code text NOT NULL DEFAULT '',
    river_job_id bigint NOT NULL UNIQUE,
    next_position integer NOT NULL DEFAULT 0 CHECK (next_position >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sync_requests_session_idx ON sync_requests(session_hash, created_at DESC);
CREATE UNIQUE INDEX sync_requests_active_idx ON sync_requests(session_hash,source_provider,source_playlist_id,destination_provider)
    WHERE state IN ('queued','running');
