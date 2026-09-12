package transfer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var ErrSyncNotFound = errors.New("sync_request_not_found")

const (
	MinSyncInterval = 15 * time.Minute
	MaxSyncInterval = 30 * 24 * time.Hour
)

type SyncArgs struct {
	SyncID string `json:"sync_id"`
}

func (SyncArgs) Kind() string { return "library_sync" }

type SyncStatus struct {
	ID                      string     `json:"id"`
	SourceProvider          string     `json:"source_provider"`
	SourcePlaylistID        string     `json:"source_playlist_id"`
	DestinationProvider     string     `json:"destination_provider"`
	DestinationPlaylistID   string     `json:"destination_playlist_id,omitempty"`
	State                   string     `json:"state"`
	ErrorCode               string     `json:"error_code,omitempty"`
	NextPosition            int        `json:"next_position"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	ScheduleIntervalSeconds int        `json:"schedule_interval_seconds,omitempty"`
	ScheduleEnabled         bool       `json:"schedule_enabled"`
	NextRunAt               *time.Time `json:"next_run_at,omitempty"`
	LastRunAt               *time.Time `json:"last_run_at,omitempty"`
}

func (s *Service) EnqueueSync(ctx context.Context, queue *river.Client[pgx.Tx], hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID string) (SyncStatus, error) {
	return s.EnqueueSyncWithSchedule(ctx, queue, hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID, 0)
}

// EnqueueSyncWithSchedule starts an immediate sync and optionally continues it
// at the requested interval after each successful run.
func (s *Service) EnqueueSyncWithSchedule(ctx context.Context, queue *river.Client[pgx.Tx], hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID string, interval time.Duration) (SyncStatus, error) {
	sourceProvider = strings.TrimSpace(sourceProvider)
	destinationProvider = strings.TrimSpace(destinationProvider)
	if sourceProvider == "" || sourcePlaylistID == "" || destinationProvider == "" {
		return SyncStatus{}, ErrInvalidProvider
	}
	if _, ok := s.providers[sourceProvider]; !ok {
		return SyncStatus{}, ErrInvalidProvider
	}
	if _, ok := s.providers[destinationProvider]; !ok {
		return SyncStatus{}, ErrInvalidProvider
	}
	if err := validateScheduleInterval(interval); err != nil {
		return SyncStatus{}, err
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	defer tx.Rollback(context.Background())
	var result SyncStatus
	err = tx.QueryRow(ctx, `SELECT id,source_provider,source_playlist_id,destination_provider,destination_playlist_id,state,error_code,next_position,created_at,updated_at,schedule_interval_seconds,schedule_enabled,next_run_at,last_run_at
		FROM sync_requests WHERE session_hash=$1 AND source_provider=$2 AND source_playlist_id=$3 AND destination_provider=$4
		AND state IN ('queued','running') LIMIT 1`, hash, sourceProvider, sourcePlaylistID, destinationProvider).Scan(
		&result.ID, &result.SourceProvider, &result.SourcePlaylistID, &result.DestinationProvider, &result.DestinationPlaylistID,
		&result.State, &result.ErrorCode, &result.NextPosition, &result.CreatedAt, &result.UpdatedAt, &result.ScheduleIntervalSeconds, &result.ScheduleEnabled, &result.NextRunAt, &result.LastRunAt)
	if err == nil {
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SyncStatus{}, err
	}
	result.ID = randomID()
	job, err := queue.InsertTx(ctx, tx, SyncArgs{SyncID: result.ID}, &river.InsertOpts{MaxAttempts: 10})
	if err != nil {
		return SyncStatus{}, err
	}
	seconds := int(interval / time.Second)
	err = tx.QueryRow(ctx, `INSERT INTO sync_requests(id,session_hash,source_provider,source_playlist_id,destination_provider,destination_playlist_id,river_job_id,schedule_interval_seconds,schedule_enabled)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,source_provider,source_playlist_id,destination_provider,destination_playlist_id,state,error_code,next_position,created_at,updated_at,schedule_interval_seconds,schedule_enabled,next_run_at,last_run_at`, result.ID, hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID, job.Job.ID, seconds, seconds > 0).Scan(
		&result.ID, &result.SourceProvider, &result.SourcePlaylistID, &result.DestinationProvider, &result.DestinationPlaylistID,
		&result.State, &result.ErrorCode, &result.NextPosition, &result.CreatedAt, &result.UpdatedAt, &result.ScheduleIntervalSeconds, &result.ScheduleEnabled, &result.NextRunAt, &result.LastRunAt)
	if err != nil {
		return SyncStatus{}, err
	}
	return result, tx.Commit(ctx)
}

func validateScheduleInterval(interval time.Duration) error {
	if interval != 0 && (interval < MinSyncInterval || interval > MaxSyncInterval) {
		return ErrInvalidSchedule
	}
	return nil
}

func (s *Service) SyncStatus(ctx context.Context, hash, id string) (SyncStatus, error) {
	var result SyncStatus
	err := s.store.pool.QueryRow(ctx, `SELECT id,source_provider,source_playlist_id,destination_provider,destination_playlist_id,state,error_code,next_position,created_at,updated_at,schedule_interval_seconds,schedule_enabled,next_run_at,last_run_at
		FROM sync_requests WHERE id=$1 AND session_hash=$2`, id, hash).Scan(
		&result.ID, &result.SourceProvider, &result.SourcePlaylistID, &result.DestinationProvider, &result.DestinationPlaylistID,
		&result.State, &result.ErrorCode, &result.NextPosition, &result.CreatedAt, &result.UpdatedAt, &result.ScheduleIntervalSeconds, &result.ScheduleEnabled, &result.NextRunAt, &result.LastRunAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSyncNotFound
	}
	return result, err
}

func (s *Service) CancelSync(ctx context.Context, hash, id string) (SyncStatus, error) {
	result, err := s.store.pool.Exec(ctx, `UPDATE sync_requests SET schedule_enabled=false,state=CASE WHEN state='running' THEN state ELSE 'cancelled' END,next_run_at=NULL,updated_at=now() WHERE id=$1 AND session_hash=$2`, id, hash)
	if err != nil {
		return SyncStatus{}, err
	}
	if result.RowsAffected() == 0 {
		return SyncStatus{}, ErrSyncNotFound
	}
	return s.SyncStatus(ctx, hash, id)
}

func (s *Service) RegisterSync(workers *river.Workers) {
	river.AddWorker(workers, &syncWorker{service: s})
}

// SyncOnce reads the source playlist, matches each entry, and reconciles the
// destination by position. Re-reading before every append makes retries safe
// after an accepted remote write and an interrupted local commit.
func (s *Service) SyncOnce(ctx context.Context, hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID string) (string, int, error) {
	source, ok := s.providers[sourceProvider]
	if !ok {
		return "", 0, ErrInvalidProvider
	}
	destination, ok := s.providers[destinationProvider]
	if !ok {
		return "", 0, ErrInvalidProvider
	}
	sourcePlaylist, err := findPlaylist(ctx, source, hash, sourcePlaylistID)
	if err != nil {
		return "", 0, err
	}
	entries, err := readAllEntries(ctx, source, hash, sourcePlaylistID)
	if err != nil {
		return "", 0, err
	}
	matchedTracks := make([]Track, 0, len(entries))
	for _, entry := range entries {
		if entry.Track == nil || entry.Unsupported || entry.Unavailable {
			return destinationPlaylistID, 0, errors.New("sync_source_entry_unavailable")
		}
		candidates, searchErr := destination.SearchTracks(ctx, hash, TrackQuery{Name: entry.Track.Name, Artists: entry.Track.Artists, ISRC: entry.Track.ISRC, DurationMS: entry.Track.DurationMS, ProviderIDs: entry.Track.ProviderIDs}, 10)
		if searchErr != nil {
			return destinationPlaylistID, 0, searchErr
		}
		result := MatchTrack(entry, destinationProvider, candidates)
		if result.Status != MatchStatusMatched || result.Matched == nil {
			return destinationPlaylistID, 0, fmt.Errorf("sync_conflict_%s", result.Status)
		}
		matchedTracks = append(matchedTracks, *result.Matched)
	}
	playlistID := destinationPlaylistID
	if playlistID == "" {
		name := sourcePlaylist.Name + " (sync)"
		playlistID, err = findPlaylistByName(ctx, destination, hash, name)
		if err != nil {
			return "", 0, err
		}
		if playlistID == "" {
			created, createErr := destination.CreatePlaylist(ctx, hash, CreatePlaylistInput{Name: name, Description: "Created by library sync"})
			if createErr != nil {
				return "", 0, createErr
			}
			playlistID = created.ID
		}
	}
	next := 0
	for _, track := range matchedTracks {
		if err := ensureEntryState(ctx, destination, hash, playlistID, next, track.ID); err != nil {
			return playlistID, next, err
		}
		next++
	}
	return playlistID, len(matchedTracks), nil
}

func findPlaylist(ctx context.Context, provider Provider, hash, playlistID string) (Playlist, error) {
	for offset := 0; offset < 10000; {
		page, err := provider.ListPlaylists(ctx, hash, offset, 50)
		if err != nil {
			return Playlist{}, err
		}
		for _, item := range page.Items {
			if item.ID == playlistID {
				return item, nil
			}
		}
		if page.NextOffset == nil {
			break
		}
		offset = *page.NextOffset
	}
	return Playlist{}, ErrSourceSnapshotNotFound
}

func readAllEntries(ctx context.Context, provider Provider, hash, playlistID string) ([]PlaylistEntry, error) {
	entries := []PlaylistEntry{}
	for offset := 0; offset < 10000; {
		page, err := provider.ReadPlaylistEntries(ctx, hash, playlistID, offset, 100)
		if err != nil {
			return nil, err
		}
		entries = append(entries, page.Items...)
		if page.NextOffset == nil {
			return entries, nil
		}
		offset = *page.NextOffset
	}
	return nil, errors.New("source_playlist_too_large")
}

func randomID() string { return rand.Text() }

type syncWorker struct {
	river.WorkerDefaults[SyncArgs]
	service *Service
}

func (w *syncWorker) Timeout(*river.Job[SyncArgs]) time.Duration { return 15 * time.Minute }

func (w *syncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
	var hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID, state string
	err := w.service.store.pool.QueryRow(ctx, `SELECT session_hash,source_provider,source_playlist_id,destination_provider,destination_playlist_id,state
		FROM sync_requests WHERE id=$1`, job.Args.SyncID).Scan(&hash, &sourceProvider, &sourcePlaylistID, &destinationProvider, &destinationPlaylistID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(ErrSyncNotFound)
	}
	if err != nil {
		return err
	}
	if state == "cancelled" {
		return river.JobCancel(errors.New("sync_cancelled"))
	}
	playlistID, _, err := w.service.SyncOnce(ctx, hash, sourceProvider, sourcePlaylistID, destinationProvider, destinationPlaylistID)
	if err == nil {
		return w.service.finishSync(ctx, job.Args.SyncID, playlistID)
	}
	code := "sync_failed"
	if errors.Is(err, ErrUnsupportedOperation) {
		code = "unsupported_operation"
	}
	if strings.HasPrefix(err.Error(), "sync_conflict_") {
		code = err.Error()
	}
	if errors.Is(err, ErrSourceSnapshotNotFound) {
		code = "source_playlist_not_found"
	}
	if errors.Is(err, ErrUnsupportedOperation) || strings.HasPrefix(err.Error(), "sync_conflict_") || errors.Is(err, ErrSourceSnapshotNotFound) || errors.Is(err, ErrInvalidProvider) || strings.HasPrefix(err.Error(), "sync_source_entry_") {
		_, _ = w.service.store.pool.Exec(ctx, "UPDATE sync_requests SET state='failed',error_code=$2,updated_at=now() WHERE id=$1", job.Args.SyncID, code)
		return river.JobCancel(err)
	}
	_, _ = w.service.store.pool.Exec(ctx, "UPDATE sync_requests SET state='failed',error_code=$2,updated_at=now() WHERE id=$1", job.Args.SyncID, code)
	return err
}

// finishSync commits the result and, for a recurring request, schedules the
// next River job in the same transaction as the next-run pointer update.
func (s *Service) finishSync(ctx context.Context, id, playlistID string) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var interval int
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT schedule_interval_seconds,schedule_enabled FROM sync_requests WHERE id=$1 FOR UPDATE`, id).Scan(&interval, &enabled); err != nil {
		return err
	}
	if interval > 0 && enabled {
		if s.queue == nil {
			return errors.New("sync_schedule_queue_unavailable")
		}
		next := time.Now().UTC().Add(time.Duration(interval) * time.Second)
		job, err := s.queue.InsertTx(ctx, tx, SyncArgs{SyncID: id}, &river.InsertOpts{MaxAttempts: 10, ScheduledAt: next})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE sync_requests SET state='queued',destination_playlist_id=$2,error_code='',last_run_at=now(),next_run_at=$3,river_job_id=$4,updated_at=now() WHERE id=$1`, id, playlistID, next, job.Job.ID)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE sync_requests SET state='completed',destination_playlist_id=$2,error_code='',last_run_at=now(),next_run_at=NULL,updated_at=now() WHERE id=$1`, id, playlistID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
