CREATE TABLE transfer_previews (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    source_provider text NOT NULL,
    source_snapshot_id text NOT NULL,
    source_playlist jsonb NOT NULL,
    destination_provider text NOT NULL,
    destination_playlist_name text NOT NULL,
    state text NOT NULL DEFAULT 'ready',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_hash, source_provider, source_snapshot_id, destination_provider)
);

CREATE TABLE transfer_preview_entries (
    preview_id text NOT NULL REFERENCES transfer_previews(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    source_entry jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('matched', 'ambiguous', 'missing', 'unsupported')),
    matched_track jsonb,
    candidate_tracks jsonb NOT NULL DEFAULT '[]'::jsonb,
    reason text NOT NULL DEFAULT '',
    PRIMARY KEY (preview_id, position)
);

CREATE TABLE transfer_runs (
    id text PRIMARY KEY,
    preview_id text NOT NULL UNIQUE REFERENCES transfer_previews(id) ON DELETE CASCADE,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    river_job_id bigint NOT NULL,
    state text NOT NULL DEFAULT 'queued',
    error_code text NOT NULL DEFAULT '',
    destination_playlist_id text NOT NULL DEFAULT '',
    next_position integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX transfer_previews_session_created_idx ON transfer_previews (session_hash, created_at DESC);
CREATE INDEX transfer_preview_entries_status_idx ON transfer_preview_entries (preview_id, status, position);
