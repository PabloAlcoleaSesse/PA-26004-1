-- Provider-neutral catalog rows. Provider IDs remain unique per service while
-- one canonical track can accumulate links to several services.
CREATE TABLE library_tracks (
    id text PRIMARY KEY,
    name text NOT NULL,
    artists jsonb NOT NULL DEFAULT '[]'::jsonb,
    album text NOT NULL DEFAULT '',
    duration_ms integer NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    isrc text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE library_track_sources (
    track_id text NOT NULL REFERENCES library_tracks(id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_id text NOT NULL,
    url text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_id)
);
CREATE INDEX library_track_sources_track_idx ON library_track_sources(track_id);
CREATE INDEX library_tracks_isrc_idx ON library_tracks(isrc) WHERE isrc <> '';

CREATE TABLE library_playlists (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    source_provider text NOT NULL,
    source_playlist_id text NOT NULL,
    name text NOT NULL,
    url text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_hash, source_provider, source_playlist_id)
);
CREATE INDEX library_playlists_session_idx ON library_playlists(session_hash, updated_at DESC);

CREATE TABLE library_playlist_entries (
    playlist_id text NOT NULL REFERENCES library_playlists(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    track_id text REFERENCES library_tracks(id) ON DELETE SET NULL,
    provider text NOT NULL,
    provider_track_id text NOT NULL DEFAULT '',
    unavailable boolean NOT NULL DEFAULT false,
    unsupported boolean NOT NULL DEFAULT false,
    raw jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (playlist_id, position)
);
CREATE INDEX library_playlist_entries_track_idx ON library_playlist_entries(track_id);
