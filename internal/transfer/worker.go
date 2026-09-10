package transfer

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type RunArgs struct {
	RunID string `json:"run_id"`
}

func (RunArgs) Kind() string { return "transfer_run" }

func (s *Service) Register(workers *river.Workers) {
	river.AddWorker(workers, &runWorker{service: s})
}

type runWorker struct {
	river.WorkerDefaults[RunArgs]
	service *Service
}

func (w *runWorker) Timeout(*river.Job[RunArgs]) time.Duration { return 10 * time.Minute }

func (w *runWorker) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	err := w.service.runOnce(ctx, job.Args.RunID)
	if err == nil {
		return nil
	}
	var rateLimit *RateLimitError
	if errors.As(err, &rateLimit) {
		delay := rateLimit.RetryAfter
		if delay <= 0 {
			delay = 30 * time.Second
		}
		return river.JobSnooze(delay)
	}
	if errors.Is(err, ErrUnsupportedOperation) {
		return river.JobCancel(err)
	}
	if errors.Is(err, ErrRunNotFound) {
		return river.JobCancel(err)
	}
	return err
}

func (s *Service) runOnce(ctx context.Context, runID string) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	status, preview, hash, err := s.store.loadRunForUpdate(ctx, tx, runID)
	if err != nil {
		return err
	}
	if status.State == "completed" {
		return tx.Commit(ctx)
	}
	provider, ok := s.providers[preview.DestinationProvider]
	if !ok {
		_ = s.store.updateRunState(ctx, tx, runID, "failed", "destination_provider_unavailable", status.DestinationPlaylistID, status.NextPosition)
		_ = tx.Commit(ctx)
		return ErrUnsupportedOperation
	}
	playlistID := status.DestinationPlaylistID
	if playlistID == "" {
		name := preview.Source.Name
		if id := status.PreviewID; len(id) > 8 {
			name += " (transfer " + id[:8] + ")"
		}
		existing, err := findPlaylistByName(ctx, provider, hash, name)
		if err != nil {
			if errors.Is(err, ErrUnsupportedOperation) {
				_ = s.store.updateRunState(ctx, tx, runID, "failed", "destination_playlist_create_unsupported", "", status.NextPosition)
				_ = tx.Commit(ctx)
				return ErrUnsupportedOperation
			}
			return err
		}
		if existing != "" {
			playlistID = existing
		} else {
			playlist, err := provider.CreatePlaylist(ctx, hash, CreatePlaylistInput{Name: name, Description: "Created by transfer preview " + status.PreviewID})
			if err != nil {
				if errors.Is(err, ErrUnsupportedOperation) {
					_ = s.store.updateRunState(ctx, tx, runID, "failed", "destination_playlist_create_unsupported", "", status.NextPosition)
					_ = tx.Commit(ctx)
					return ErrUnsupportedOperation
				}
				return err
			}
			playlistID = playlist.ID
		}
	}
	matchedEntries := make([]PreviewEntry, 0, len(preview.Entries))
	for _, entry := range preview.Entries {
		if entry.Status == MatchStatusMatched && entry.Matched != nil {
			matchedEntries = append(matchedEntries, entry)
		}
	}
	next := status.NextPosition
	for next < len(matchedEntries) {
		entry := matchedEntries[next]
		if err := ensureEntryState(ctx, provider, hash, playlistID, next, entry.Matched.ID); err != nil {
			if errors.Is(err, ErrUnsupportedOperation) {
				_ = s.store.updateRunState(ctx, tx, runID, "failed", "destination_reconciliation_unsupported", playlistID, next)
				_ = tx.Commit(ctx)
				return ErrUnsupportedOperation
			}
			return err
		}
		next++
	}
	if err := s.store.updateRunState(ctx, tx, runID, "completed", "", playlistID, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func findPlaylistByName(ctx context.Context, provider Provider, hash, name string) (string, error) {
	offset := 0
	for {
		page, err := provider.ListPlaylists(ctx, hash, offset, 50)
		if err != nil {
			return "", err
		}
		for _, playlist := range page.Items {
			if playlist.Name == name {
				return playlist.ID, nil
			}
		}
		if page.NextOffset == nil {
			return "", nil
		}
		offset = *page.NextOffset
	}
}

func ensureEntryState(ctx context.Context, provider Provider, hash, playlistID string, position int, trackID string) error {
	page, err := provider.ReadPlaylistEntries(ctx, hash, playlistID, position, 1)
	if err != nil {
		return err
	}
	if len(page.Items) > 0 && page.Items[0].Track != nil {
		if page.Items[0].Track.ID == trackID {
			return nil
		}
		return errors.New("destination_playlist_unexpected_order")
	}
	return provider.AddTracksToPlaylist(ctx, hash, playlistID, []string{trackID})
}

func (s *Store) updateRunProgress(ctx context.Context, runID, state, errorCode, playlistID string, nextPosition int) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err := s.updateRunState(ctx, tx, runID, state, errorCode, playlistID, nextPosition); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
