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

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/applemusic"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/config"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/httpapi"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/jobs"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/library"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/spotify"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/transfer"
	"github.com/riverqueue/river"
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
	appleConfig, err := config.LoadAppleMusic()
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
		for _, query := range []string{
			"SELECT id FROM river_job LIMIT 0",
			"SELECT session_hash FROM spotify_connections LIMIT 0",
			"SELECT import_id FROM spotify_snapshots LIMIT 0",
			"SELECT id FROM transfer_previews LIMIT 0",
			"SELECT id FROM library_playlists LIMIT 0",
			"SELECT id FROM apple_imports LIMIT 0",
		} {
			if _, err := pool.Exec(ctx, query); err != nil {
				return err
			}
		}
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(ready))
	library.RegisterHTTP(mux, library.NewStore(pool))

	origin := os.Getenv("PUBLIC_ORIGIN")
	if origin == "" {
		origin = "http://" + cfg.HTTPAddr
	}

	var register []func(*river.Workers)
	var spotifyAuth *spotify.Auth
	var spotifyImporter *spotify.Importer
	var appleImporter *applemusic.Importer
	var transferService *transfer.Service
	transferProviders := []transfer.Provider{}

	if spotifyConfig.ClientID != "" {
		store, err := spotify.NewPostgresStore(pool, spotifyConfig.EncryptionKey)
		if err != nil {
			return err
		}
		client := spotify.NewClient(spotifyConfig.ClientID, spotifyConfig.RedirectURI)
		spotifyImporter = spotify.NewImporter(pool, store, client)
		spotifyAuth = spotify.NewAuth(client, store)
		spotifyAuth.Register(mux)
		register = append(register, spotifyImporter.Register)
		transferProviders = append(transferProviders, transfer.NewSpotifyProvider(client, store))
		logger.Info("Spotify connection enabled")
	} else {
		logger.Info("Spotify connection disabled; set SPOTIFY_CLIENT_ID to enable")
	}
	if appleConfig.TeamID != "" {
		key, err := config.LoadTokenEncryptionKey()
		if err != nil {
			return err
		}
		store, err := applemusic.NewPostgresStore(pool, key)
		if err != nil {
			return err
		}
		client, err := applemusic.NewClient(appleConfig.TeamID, appleConfig.KeyID, appleConfig.PrivateKeyPEM)
		if err != nil {
			return err
		}
		client.Register(mux, store, origin)
		appleImporter = applemusic.NewImporter(pool, store, client)
		register = append(register, appleImporter.Register)
		transferProviders = append(transferProviders, transfer.NewAppleMusicProvider(client, store))
		logger.Info("Apple Music connection enabled")
	}

	if spotifyImporter != nil {
		if len(transferProviders) > 0 {
			transferService = transfer.NewService(transfer.NewStore(pool), transferProviders...)
			register = append(register, transferService.Register)
		}
	}
	if len(register) > 0 {
		queue, err := jobs.NewClient(pool, logger, 0, register...)
		if err != nil {
			return err
		}
		if spotifyAuth != nil && spotifyImporter != nil {
			spotifyAuth.RegisterImports(mux, spotifyImporter, queue)
		}
		if appleImporter != nil {
			applemusic.RegisterImports(mux, appleImporter, queue)
		}
		if transferService != nil {
			transfer.RegisterHTTP(mux, transferService, queue, origin)
		}
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
