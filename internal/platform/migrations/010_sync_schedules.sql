-- Optional recurring execution for a sync request. The River payload remains
-- an opaque sync ID; the schedule is kept in application data so it can be
-- inspected and disabled without changing queued job arguments.
ALTER TABLE sync_requests
    ADD COLUMN schedule_interval_seconds integer NOT NULL DEFAULT 0,
    ADD COLUMN schedule_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN next_run_at timestamptz,
    ADD COLUMN last_run_at timestamptz;

ALTER TABLE sync_requests
    ADD CONSTRAINT sync_requests_schedule_interval_check
    CHECK (schedule_interval_seconds = 0 OR schedule_interval_seconds BETWEEN 900 AND 2592000);

CREATE INDEX sync_requests_schedule_due_idx ON sync_requests(next_run_at)
    WHERE schedule_enabled = true;
