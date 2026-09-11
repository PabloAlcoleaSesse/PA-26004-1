-- A manual decision is kept separately from the generated preview so the
-- user's choice survives a preview refresh and can be audited safely.
CREATE TABLE transfer_match_decisions (
    preview_id text NOT NULL REFERENCES transfer_previews(id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    session_hash text NOT NULL CHECK (length(session_hash) = 64),
    destination_provider text NOT NULL,
    selected_track jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (preview_id, position)
);

CREATE INDEX transfer_match_decisions_session_idx
    ON transfer_match_decisions (session_hash, updated_at DESC);
