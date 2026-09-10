package transfer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/spotify"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

var ErrPreviewNotFound = errors.New("transfer_preview_not_found")
var ErrRunNotFound = errors.New("transfer_run_not_found")
var ErrSourceSnapshotNotFound = errors.New("source_snapshot_not_found")

// Store persists transfer previews and transfer execution state.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) LoadSpotifySnapshot(ctx context.Context, hash, importID string) (Playlist, []PlaylistEntry, error) {
	var source Playlist
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return source, nil, err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `SELECT snap.playlist_id,snap.name,snap.spotify_url,snap.snapshot_id
		FROM spotify_snapshots snap
		JOIN spotify_imports imp ON imp.id = snap.import_id
		JOIN spotify_connections conn ON conn.session_hash = imp.session_hash
		WHERE snap.import_id = $1 AND imp.session_hash = $2 AND conn.expires_at > now()`, importID, hash,
	).Scan(&source.ID, &source.Name, &source.URL, &source.SnapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, nil, ErrSourceSnapshotNotFound
	}
	if err != nil {
		return source, nil, err
	}
	source.Provider = ProviderSpotify
	rows, err := tx.Query(ctx, "SELECT item FROM spotify_snapshot_entries WHERE import_id = $1 ORDER BY position", importID)
	if err != nil {
		return source, nil, err
	}
	defer rows.Close()
	entries := []PlaylistEntry{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return source, nil, err
		}
		var raw spotify.Entry
		if err := json.Unmarshal(body, &raw); err != nil {
			return source, nil, err
		}
		entries = append(entries, fromSpotifyEntry(raw))
	}
	if err := rows.Err(); err != nil {
		return source, nil, err
	}
	return source, entries, tx.Commit(ctx)
}

func (s *Store) SavePreview(ctx context.Context, hash, sourceSnapshotID, destinationProvider string, source Playlist, entries []PreviewEntry) (TransferPreview, error) {
	id := rand.Text()
	preview := TransferPreview{
		ID:                  id,
		Source:              source,
		DestinationProvider: destinationProvider,
		State:               "ready",
		CreatedAt:           time.Now().UTC(),
		Entries:             entries,
	}
	suffix := id
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	destinationName := source.Name + " (transfer " + suffix + ")"
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TransferPreview{}, err
	}
	defer tx.Rollback(context.Background())
	sourceJSON, err := json.Marshal(source)
	if err != nil {
		return TransferPreview{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO transfer_previews (id,session_hash,source_provider,source_snapshot_id,source_playlist,destination_provider,destination_playlist_name,state,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (session_hash, source_provider, source_snapshot_id, destination_provider)
		DO UPDATE SET created_at = now()
		RETURNING id, state, created_at`, id, hash, source.Provider, sourceSnapshotID, sourceJSON, destinationProvider, destinationName, "ready", preview.CreatedAt).Scan(&preview.ID, &preview.State, &preview.CreatedAt)
	if err != nil {
		return TransferPreview{}, err
	}
	batch := &pgx.Batch{}
	for _, entry := range entries {
		sourceEntry, err := json.Marshal(entry.Source)
		if err != nil {
			return TransferPreview{}, err
		}
		matched := []byte("null")
		if entry.Matched != nil {
			if matched, err = json.Marshal(entry.Matched); err != nil {
				return TransferPreview{}, err
			}
		}
		candidates, err := json.Marshal(entry.Candidates)
		if err != nil {
			return TransferPreview{}, err
		}
		batch.Queue(`INSERT INTO transfer_preview_entries (preview_id,position,source_entry,status,matched_track,candidate_tracks,reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (preview_id,position)
			DO UPDATE SET source_entry = excluded.source_entry, status = excluded.status, matched_track = excluded.matched_track, candidate_tracks = excluded.candidate_tracks, reason = excluded.reason`,
			preview.ID, entry.Position, sourceEntry, string(entry.Status), matched, candidates, entry.Reason)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return TransferPreview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TransferPreview{}, err
	}
	return preview, nil
}

func (s *Store) LoadPreview(ctx context.Context, hash, previewID string, offset, limit int) (TransferPreview, error) {
	if offset < 0 || limit < 1 || limit > 500 {
		return TransferPreview{}, errors.New("invalid_preview_pagination")
	}
	var preview TransferPreview
	var sourceJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT id,source_playlist,destination_provider,state,created_at
		FROM transfer_previews WHERE id = $1 AND session_hash = $2`, previewID, hash).Scan(&preview.ID, &sourceJSON, &preview.DestinationProvider, &preview.State, &preview.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TransferPreview{}, ErrPreviewNotFound
	}
	if err != nil {
		return TransferPreview{}, err
	}
	if err := json.Unmarshal(sourceJSON, &preview.Source); err != nil {
		return TransferPreview{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT position,source_entry,status,matched_track,candidate_tracks,reason
		FROM transfer_preview_entries WHERE preview_id = $1 AND position >= $2 ORDER BY position LIMIT $3`, preview.ID, offset, limit)
	if err != nil {
		return TransferPreview{}, err
	}
	defer rows.Close()
	preview.Entries = []PreviewEntry{}
	for rows.Next() {
		var entry PreviewEntry
		var sourceEntryJSON, matchedTrackJSON, candidateTracksJSON []byte
		var status string
		if err := rows.Scan(&entry.Position, &sourceEntryJSON, &status, &matchedTrackJSON, &candidateTracksJSON, &entry.Reason); err != nil {
			return TransferPreview{}, err
		}
		entry.Status = MatchStatus(status)
		if err := json.Unmarshal(sourceEntryJSON, &entry.Source); err != nil {
			return TransferPreview{}, err
		}
		if string(matchedTrackJSON) != "null" {
			entry.Matched = &Track{}
			if err := json.Unmarshal(matchedTrackJSON, entry.Matched); err != nil {
				return TransferPreview{}, err
			}
		}
		if err := json.Unmarshal(candidateTracksJSON, &entry.Candidates); err != nil {
			return TransferPreview{}, err
		}
		preview.Entries = append(preview.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return TransferPreview{}, err
	}
	return preview, nil
}

func (s *Store) EnqueueRun(ctx context.Context, queue *river.Client[pgx.Tx], hash, previewID string) (TransferRunStatus, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TransferRunStatus{}, err
	}
	defer tx.Rollback(context.Background())
	var previewExists bool
	if err := tx.QueryRow(ctx, "SELECT true FROM transfer_previews WHERE id = $1 AND session_hash = $2", previewID, hash).Scan(&previewExists); errors.Is(err, pgx.ErrNoRows) {
		return TransferRunStatus{}, ErrPreviewNotFound
	} else if err != nil {
		return TransferRunStatus{}, err
	}
	var result TransferRunStatus
	err = tx.QueryRow(ctx, `SELECT id,preview_id,state,error_code,destination_playlist_id,next_position,created_at,updated_at
		FROM transfer_runs WHERE preview_id = $1 AND session_hash = $2`, previewID, hash).Scan(
		&result.ID, &result.PreviewID, &result.State, &result.ErrorCode, &result.DestinationPlaylistID,
		&result.NextPosition, &result.CreatedAt, &result.UpdatedAt,
	)
	if err == nil {
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TransferRunStatus{}, err
	}
	result.ID = rand.Text()
	job, err := queue.InsertTx(ctx, tx, RunArgs{RunID: result.ID}, &river.InsertOpts{MaxAttempts: 10})
	if err != nil {
		return TransferRunStatus{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO transfer_runs (id,preview_id,session_hash,river_job_id,state,error_code,destination_playlist_id,next_position)
		VALUES ($1,$2,$3,$4,$5,'','',0)
		RETURNING id,preview_id,state,error_code,destination_playlist_id,next_position,created_at,updated_at`, result.ID, previewID, hash, job.Job.ID, "queued").Scan(
		&result.ID, &result.PreviewID, &result.State, &result.ErrorCode, &result.DestinationPlaylistID,
		&result.NextPosition, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return TransferRunStatus{}, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) LoadRunStatus(ctx context.Context, hash, runID string) (TransferRunStatus, error) {
	var status TransferRunStatus
	err := s.pool.QueryRow(ctx, `SELECT id,preview_id,state,error_code,destination_playlist_id,next_position,created_at,updated_at
		FROM transfer_runs WHERE id = $1 AND session_hash = $2`, runID, hash).Scan(
		&status.ID, &status.PreviewID, &status.State, &status.ErrorCode, &status.DestinationPlaylistID,
		&status.NextPosition, &status.CreatedAt, &status.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TransferRunStatus{}, ErrRunNotFound
	}
	return status, err
}

func (s *Store) loadRunForUpdate(ctx context.Context, tx pgx.Tx, runID string) (TransferRunStatus, TransferPreview, string, error) {
	var status TransferRunStatus
	var preview TransferPreview
	var sourceJSON []byte
	var hash string
	err := tx.QueryRow(ctx, `SELECT run.id,run.preview_id,run.state,run.error_code,run.destination_playlist_id,run.next_position,run.created_at,run.updated_at,
		preview.session_hash,preview.source_playlist,preview.destination_provider,preview.state,preview.created_at
		FROM transfer_runs run JOIN transfer_previews preview ON preview.id = run.preview_id
		WHERE run.id = $1 FOR UPDATE`, runID).Scan(
		&status.ID, &status.PreviewID, &status.State, &status.ErrorCode, &status.DestinationPlaylistID,
		&status.NextPosition, &status.CreatedAt, &status.UpdatedAt,
		&hash, &sourceJSON, &preview.DestinationProvider, &preview.State, &preview.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, preview, "", ErrRunNotFound
	}
	if err != nil {
		return status, preview, "", err
	}
	preview.ID = status.PreviewID
	if err := json.Unmarshal(sourceJSON, &preview.Source); err != nil {
		return status, preview, "", err
	}
	entries, err := loadPreviewEntriesTx(ctx, tx, preview.ID)
	if err != nil {
		return status, preview, "", err
	}
	preview.Entries = entries
	return status, preview, hash, nil
}

func loadPreviewEntriesTx(ctx context.Context, tx pgx.Tx, previewID string) ([]PreviewEntry, error) {
	rows, err := tx.Query(ctx, `SELECT position,source_entry,status,matched_track,candidate_tracks,reason
		FROM transfer_preview_entries WHERE preview_id = $1 ORDER BY position`, previewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []PreviewEntry{}
	for rows.Next() {
		var entry PreviewEntry
		var sourceEntryJSON, matchedTrackJSON, candidateTracksJSON []byte
		var status string
		if err := rows.Scan(&entry.Position, &sourceEntryJSON, &status, &matchedTrackJSON, &candidateTracksJSON, &entry.Reason); err != nil {
			return nil, err
		}
		entry.Status = MatchStatus(status)
		if err := json.Unmarshal(sourceEntryJSON, &entry.Source); err != nil {
			return nil, err
		}
		if string(matchedTrackJSON) != "null" {
			entry.Matched = &Track{}
			if err := json.Unmarshal(matchedTrackJSON, entry.Matched); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(candidateTracksJSON, &entry.Candidates); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) updateRunState(ctx context.Context, tx pgx.Tx, runID, state, errorCode, playlistID string, nextPosition int) error {
	_, err := tx.Exec(ctx, `UPDATE transfer_runs SET state = $2,error_code = $3,destination_playlist_id = $4,next_position = $5,updated_at = now()
		WHERE id = $1`, runID, state, errorCode, playlistID, nextPosition)
	if err != nil {
		return fmt.Errorf("update transfer run: %w", err)
	}
	return nil
}

func fromSpotifyEntry(entry spotify.Entry) PlaylistEntry {
	result := PlaylistEntry{Position: entry.Position, AddedAt: entry.AddedAt, Unavailable: entry.Unavailable, Unsupported: entry.Unsupported}
	if entry.ID == "" && entry.Name == "" {
		return result
	}
	artists := make([]string, 0, len(entry.Artists))
	for _, artist := range entry.Artists {
		artists = append(artists, artist.Name)
	}
	result.Track = &Track{
		Provider:   ProviderSpotify,
		ID:         entry.ID,
		Name:       entry.Name,
		Artists:    artists,
		Album:      entry.Album,
		DurationMS: entry.DurationMS,
		ISRC:       entry.ISRC,
		ProviderIDs: map[string]string{
			ProviderSpotify: entry.ID,
		},
		URL: entry.SpotifyURL,
	}
	if entry.LinkedFromID != "" {
		result.Track.ProviderIDs[ProviderSpotify] = entry.LinkedFromID
	}
	if entry.Unsupported {
		result.Track.Provider = entry.Type
	}
	return result
}
