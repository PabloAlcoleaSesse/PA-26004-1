package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/config"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/httpapi"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/jobs"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/spotify"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := platform.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	ready := func(ctx context.Context) error {
		// Check both schemas before accepting traffic on a migrated deployment.
		_, err := pool.Exec(ctx, "SELECT id FROM river_job LIMIT 0")
		if err == nil {
			_, err = pool.Exec(ctx, "SELECT session_hash FROM spotify_connections LIMIT 0")
		}
		if err == nil {
			_, err = pool.Exec(ctx, "SELECT import_id FROM spotify_snapshots LIMIT 0")
		}
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(ready))
	if spotifyConfig.ClientID != "" {
		store, err := spotify.NewPostgresStore(pool, spotifyConfig.EncryptionKey)
		if err != nil {
			return err
		}
		client := spotify.NewClient(spotifyConfig.ClientID, spotifyConfig.RedirectURI)
		importer := spotify.NewImporter(pool, store, client)
		queue, err := jobs.NewClient(pool, logger, 0, importer.Register)
		if err != nil {
			return err
		}
		auth := spotify.NewAuth(client, store)
		auth.Register(mux)
		auth.RegisterImports(mux, importer, queue)
		logger.Info("Spotify connection enabled")
	} else {
		logger.Info("Spotify connection disabled; set SPOTIFY_CLIENT_ID to enable")
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("api starting", "address", cfg.HTTPAddr)
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		logger.Info("api stopped gracefully")
		return nil
	}
}
