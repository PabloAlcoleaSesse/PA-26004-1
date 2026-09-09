package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/config"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/jobs"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/spotify"
	"github.com/riverqueue/river"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	spotifyConfig, err := config.LoadSpotify()
	if err != nil {
		return err
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := platform.OpenDatabase(signals, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	var register []func(*river.Workers)
	if spotifyConfig.ClientID != "" {
		store, err := spotify.NewPostgresStore(pool, spotifyConfig.EncryptionKey)
		if err != nil {
			return err
		}
		provider := spotify.NewClient(spotifyConfig.ClientID, spotifyConfig.RedirectURI)
		importer := spotify.NewImporter(pool, store, provider)
		register = append(register, importer.Register)
	}
	client, err := jobs.NewClient(pool, logger, cfg.WorkerConcurrency, register...)
	if err != nil {
		return err
	}
	// Signals initiate a drain; they must not immediately cancel running jobs.
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	if err := client.Start(workCtx); err != nil {
		return err
	}
	logger.Info("worker started", "concurrency", cfg.WorkerConcurrency)
	<-signals.Done()
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := client.Stop(shutdownCtx); err != nil {
		logger.Warn("worker drain timed out; cancelling active jobs")
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return client.StopAndCancel(cancelCtx)
	}
	logger.Info("worker stopped gracefully")
	return nil
}
