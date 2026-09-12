package applemusic

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/library"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

var ErrImportNotFound = errors.New("apple_import_not_found")

type ImportArgs struct {
	ImportID string `json:"import_id"`
}

func (ImportArgs) Kind() string { return "apple_music_playlist_import" }

type ImportStatus struct {
	ID         string `json:"id"`
	PlaylistID string `json:"playlist_id"`
	State      string `json:"state"`
	ErrorCode  string `json:"error_code,omitempty"`
}

type Importer struct {
	pool   *pgxpool.Pool
	store  Store
	client *Client
}

func NewImporter(pool *pgxpool.Pool, store Store, client *Client) *Importer {
	return &Importer{pool: pool, store: store, client: client}
}

func (i *Importer) Register(workers *river.Workers) {
	river.AddWorker(workers, &importWorker{importer: i})
}

func (i *Importer) Enqueue(ctx context.Context, queue *river.Client[pgx.Tx], hash, playlistID string) (string, error) {
	if playlistID == "" || len(playlistID) > 128 {
		return "", errors.New("invalid Apple Music playlist ID")
	}
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	if _, err := i.store.Load(ctx, hash); err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT i.id FROM apple_imports i JOIN river_job j ON j.id = i.river_job_id
		WHERE i.session_hash = $1 AND i.playlist_id = $2 AND j.state IN ('available','pending','running','retryable','scheduled') LIMIT 1`, hash, playlistID).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	id := rand.Text()
	job, err := queue.InsertTx(ctx, tx, ImportArgs{ImportID: id}, &river.InsertOpts{MaxAttempts: 5})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO apple_imports(id,session_hash,playlist_id,river_job_id) VALUES($1,$2,$3,$4)`, id, hash, playlistID, job.Job.ID); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (i *Importer) Status(ctx context.Context, hash, id string) (ImportStatus, error) {
	var result ImportStatus
	err := i.pool.QueryRow(ctx, `SELECT i.id,i.playlist_id,
		CASE WHEN i.completed_at IS NOT NULL THEN 'completed'
		ELSE COALESCE(j.state::text,'expired') END, i.error_code
		FROM apple_imports i LEFT JOIN river_job j ON j.id=i.river_job_id WHERE i.id=$1 AND i.session_hash=$2`, id, hash).
		Scan(&result.ID, &result.PlaylistID, &result.State, &result.ErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrImportNotFound
	}
	return result, err
}

func (i *Importer) Run(ctx context.Context, id string) error {
	var hash, playlistID, storefront, userToken string
	err := i.pool.QueryRow(ctx, `SELECT i.session_hash,i.playlist_id FROM apple_imports i WHERE i.id=$1`, id).Scan(&hash, &playlistID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrImportNotFound
	}
	if err != nil {
		return err
	}
	connection, err := i.store.Load(ctx, hash)
	if err != nil {
		return err
	}
	userToken, storefront = connection.UserToken, connection.Storefront
	var selected Playlist
	for offset := 0; ; offset += 100 {
		if offset >= 1000 {
			return errors.New("apple_playlist_index_too_large")
		}
		playlists, err := i.client.Playlists(ctx, userToken, storefront, offset, 100)
		if err != nil {
			return err
		}
		for _, playlist := range playlists.Data {
			if playlist.ID == playlistID {
				selected = playlist
				break
			}
		}
		if selected.ID != "" || len(playlists.Data) < 100 {
			break
		}
	}
	if selected.ID == "" {
		return errors.New("apple_playlist_not_found")
	}
	entries := make([]library.SnapshotEntry, 0, 100)
	for offset := 0; ; offset += 100 {
		if offset >= 10000 {
			return errors.New("apple_playlist_too_large")
		}
		page, err := i.client.PlaylistSongs(ctx, userToken, storefront, playlistID, offset, 100)
		if err != nil {
			return err
		}
		for n, song := range page.Data {
			entries = append(entries, library.SnapshotEntry{Position: offset + n, ProviderID: song.ID, Name: song.Attributes.Name,
				Artists: []string{song.Attributes.ArtistName}, Album: song.Attributes.AlbumName, DurationMS: song.Attributes.DurationMS,
				ISRC: song.Attributes.ISRC, URL: song.Attributes.URL, Raw: song})
		}
		if len(page.Data) < 100 {
			break
		}
	}
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err := library.SyncPlaylistTx(ctx, tx, hash, "apple-music", playlistID, selected.Attributes.Name, selected.Attributes.URL, entries); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE apple_imports SET error_code='',completed_at=now() WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type importWorker struct {
	river.WorkerDefaults[ImportArgs]
	importer *Importer
}

func (w *importWorker) Timeout(*river.Job[ImportArgs]) time.Duration { return 5 * time.Minute }

func (w *importWorker) Work(ctx context.Context, job *river.Job[ImportArgs]) error {
	err := w.importer.Run(ctx, job.Args.ImportID)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrImportNotFound) || errors.Is(err, ErrNoConnection) {
		return river.JobCancel(err)
	}
	code := "apple_import_failed"
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.Status == 401 || providerErr.Status == 403 {
			code = "reconnect_apple_music"
			_, _ = w.importer.pool.Exec(ctx, "UPDATE apple_imports SET error_code=$2 WHERE id=$1", job.Args.ImportID, code)
			return river.JobCancel(err)
		}
		if providerErr.Status == 404 {
			code = "apple_playlist_not_found"
			_, _ = w.importer.pool.Exec(ctx, "UPDATE apple_imports SET error_code=$2 WHERE id=$1", job.Args.ImportID, code)
			return river.JobCancel(err)
		}
		if providerErr.Status == 429 {
			code = fmt.Sprintf("apple_rate_limited_%d", providerErr.RetryAfter)
			_, _ = w.importer.pool.Exec(ctx, "UPDATE apple_imports SET error_code=$2 WHERE id=$1", job.Args.ImportID, code)
			if providerErr.RetryAfter > 0 {
				return river.JobSnooze(time.Duration(providerErr.RetryAfter) * time.Second)
			}
		}
	}
	_, _ = w.importer.pool.Exec(ctx, "UPDATE apple_imports SET error_code=$2 WHERE id=$1", job.Args.ImportID, code)
	return err
}
