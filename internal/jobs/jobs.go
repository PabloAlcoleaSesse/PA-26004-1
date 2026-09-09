package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// ProbeArgs verifies queue delivery without touching music-service accounts.
type ProbeArgs struct{}

func (ProbeArgs) Kind() string { return "system_probe" }

type probeWorker struct {
	river.WorkerDefaults[ProbeArgs]
	logger *slog.Logger
}

func (w *probeWorker) Work(ctx context.Context, job *river.Job[ProbeArgs]) error {
	w.logger.InfoContext(ctx, "queue probe processed", "job_id", job.ID)
	return nil
}

// NewClient with concurrency zero creates an insert-only client.
func NewClient(pool *pgxpool.Pool, logger *slog.Logger, concurrency int) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &probeWorker{logger: logger})
	cfg := &river.Config{Logger: logger, Workers: workers, JobTimeout: time.Minute, MaxAttempts: 5}
	if concurrency > 0 {
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: concurrency}}
	}
	return river.NewClient(riverpgxv5.New(pool), cfg)
}
