-- Immutable listening events imported from providers. The provider event key
-- makes repeated polling idempotent while retaining repeated plays.
CREATE TABLE listening_events (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    played_at timestamptz NOT NULL,
    track_id text REFERENCES library_tracks(id) ON DELETE SET NULL,
    provider_track_id text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    artists jsonb NOT NULL DEFAULT '[]'::jsonb,
    album text NOT NULL DEFAULT '',
    duration_ms integer NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    isrc text NOT NULL DEFAULT '',
    raw jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_hash, provider, provider_event_id)
);
CREATE INDEX listening_events_session_time_idx ON listening_events(session_hash, played_at DESC);
CREATE INDEX listening_events_session_provider_idx ON listening_events(session_hash, provider, played_at DESC);

CREATE TABLE listening_imports (
    id text PRIMARY KEY,
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    provider text NOT NULL,
    river_job_id bigint NOT NULL UNIQUE,
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','completed','failed')),
    error_code text NOT NULL DEFAULT '',
    imported_count integer NOT NULL DEFAULT 0 CHECK (imported_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
CREATE INDEX listening_imports_session_idx ON listening_imports(session_hash, created_at DESC);
