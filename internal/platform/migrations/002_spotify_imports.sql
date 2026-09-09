-- Import requests and immutable snapshots belong to the same browser session
-- as its credentials. Disconnect/reconnection removes the associated data.
CREATE TABLE spotify_imports (
    id text PRIMARY KEY,
    session_hash text NOT NULL REFERENCES spotify_connections(session_hash) ON DELETE CASCADE,
    playlist_id text NOT NULL,
    river_job_id bigint NOT NULL,
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX spotify_imports_owner_idx ON spotify_imports(session_hash, playlist_id);

CREATE TABLE spotify_snapshots (
    import_id text PRIMARY KEY REFERENCES spotify_imports(id) ON DELETE CASCADE,
    playlist_id text NOT NULL,
    snapshot_id text NOT NULL,
    name text NOT NULL,
    spotify_url text NOT NULL,
    item_count integer NOT NULL CHECK (item_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Position, not track ID, is the key: duplicates are meaningful playlist data.
CREATE TABLE spotify_snapshot_entries (
    import_id text NOT NULL REFERENCES spotify_snapshots(import_id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    item jsonb NOT NULL,
    PRIMARY KEY (import_id, position)
);
