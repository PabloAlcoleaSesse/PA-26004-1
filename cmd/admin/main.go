package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/config"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/jobs"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("admin command failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) != 2 || (os.Args[1] != "migrate" && os.Args[1] != "probe") {
		return errors.New("usage: admin migrate|probe")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, time.Minute)
	defer cancel()
	pool, err := platform.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if os.Args[1] == "migrate" {
		migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: logger})
		if err != nil {
			return err
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return err
		}
		logger.Info("database migrations applied")
		return nil
	}
	client, err := jobs.NewClient(pool, logger, 0)
	if err != nil {
		return err
	}
	result, err := client.Insert(ctx, jobs.ProbeArgs{}, nil)
	if err != nil {
		return err
	}
	logger.Info("queue probe inserted", "job_id", result.Job.ID)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for probe completion (is the worker running?): %w", ctx.Err())
		case <-ticker.C:
			job, err := client.JobGet(ctx, result.Job.ID)
			if err != nil {
				return err
			}
			switch job.State {
			case rivertype.JobStateCompleted:
				logger.Info("queue probe completed", "job_id", job.ID)
				return nil
			case rivertype.JobStateCancelled, rivertype.JobStateDiscarded:
				return fmt.Errorf("queue probe ended in state %s", job.State)
			}
		}
	}
}
