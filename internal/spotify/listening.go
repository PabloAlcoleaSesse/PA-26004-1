package spotify

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/library"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

var ErrListeningImportNotFound = errors.New("spotify_listening_import_not_found")

type ListeningImportArgs struct {
	ImportID string `json:"import_id"`
}

func (ListeningImportArgs) Kind() string { return "spotify_listening_import" }

type ListeningImportStatus struct {
	ID            string     `json:"id"`
	Provider      string     `json:"provider"`
	State         string     `json:"state"`
	ErrorCode     string     `json:"error_code,omitempty"`
	ImportedCount int        `json:"imported_count"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type ListeningImporter struct {
	pool   *pgxpool.Pool
	store  Store
	client *Client
}

func NewListeningImporter(pool *pgxpool.Pool, store Store, client *Client) *ListeningImporter {
	return &ListeningImporter{pool: pool, store: store, client: client}
}

func (i *ListeningImporter) Register(workers *river.Workers) {
	river.AddWorker(workers, &listeningWorker{importer: i})
}

func (i *ListeningImporter) Enqueue(ctx context.Context, queue *river.Client[pgx.Tx], hash string) (ListeningImportStatus, error) {
	if err := i.store.Update(ctx, hash, func(*Connection) error { return nil }); err != nil {
		return ListeningImportStatus{}, err
	}
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return ListeningImportStatus{}, err
	}
	defer tx.Rollback(context.Background())
	var existing ListeningImportStatus
	err = tx.QueryRow(ctx, `SELECT id,provider,state,error_code,imported_count,created_at,completed_at
		FROM listening_imports WHERE session_hash=$1 AND provider='spotify' AND state IN ('queued','running') ORDER BY created_at DESC LIMIT 1`, hash).Scan(
		&existing.ID, &existing.Provider, &existing.State, &existing.ErrorCode, &existing.ImportedCount, &existing.CreatedAt, &existing.CompletedAt)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ListeningImportStatus{}, err
	}
	id := rand.Text()
	job, err := queue.InsertTx(ctx, tx, ListeningImportArgs{ImportID: id}, &river.InsertOpts{MaxAttempts: 5})
	if err != nil {
		return ListeningImportStatus{}, err
	}
	var result ListeningImportStatus
	err = tx.QueryRow(ctx, `INSERT INTO listening_imports(id,session_hash,provider,river_job_id) VALUES($1,$2,'spotify',$3)
		RETURNING id,provider,state,error_code,imported_count,created_at,completed_at`, id, hash, job.Job.ID).Scan(
		&result.ID, &result.Provider, &result.State, &result.ErrorCode, &result.ImportedCount, &result.CreatedAt, &result.CompletedAt)
	if err != nil {
		return ListeningImportStatus{}, err
	}
	return result, tx.Commit(ctx)
}

func (i *ListeningImporter) Status(ctx context.Context, hash, id string) (ListeningImportStatus, error) {
	var result ListeningImportStatus
	err := i.pool.QueryRow(ctx, `SELECT id,provider,state,error_code,imported_count,created_at,completed_at
		FROM listening_imports WHERE id=$1 AND session_hash=$2`, id, hash).Scan(
		&result.ID, &result.Provider, &result.State, &result.ErrorCode, &result.ImportedCount, &result.CreatedAt, &result.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrListeningImportNotFound
	}
	return result, err
}

func (i *ListeningImporter) Run(ctx context.Context, id string) error {
	var hash string
	if err := i.pool.QueryRow(ctx, "SELECT session_hash FROM listening_imports WHERE id=$1 AND provider='spotify'", id).Scan(&hash); errors.Is(err, pgx.ErrNoRows) {
		return ErrListeningImportNotFound
	} else if err != nil {
		return err
	}
	page, err := i.client.RecentlyPlayed(ctx, i.store, hash, nil, 50)
	if err != nil {
		return err
	}
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "UPDATE listening_imports SET state='running' WHERE id=$1", id); err != nil {
		return err
	}
	count := 0
	for _, played := range page.Items {
		if played.Track.ID == "" || played.PlayedAt.IsZero() {
			continue
		}
		trackID := library.TrackID("spotify", played.Track.ID)
		artists := make([]string, 0, len(played.Track.Artists))
		for _, artist := range played.Track.Artists {
			artists = append(artists, artist.Name)
		}
		artistsJSON, err := json.Marshal(artists)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO library_tracks(id,name,artists,album,duration_ms,isrc,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,artists=excluded.artists,album=excluded.album,duration_ms=excluded.duration_ms,isrc=excluded.isrc,updated_at=now()`, trackID, played.Track.Name, artistsJSON, played.Track.Album, played.Track.DurationMS, played.Track.ISRC); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO library_track_sources(track_id,provider,provider_id,url) VALUES($1,'spotify',$2,$3)
			ON CONFLICT(provider,provider_id) DO UPDATE SET track_id=excluded.track_id,url=excluded.url`, trackID, played.Track.ID, played.Track.SpotifyURL); err != nil {
			return err
		}
		raw, err := json.Marshal(played)
		if err != nil {
			return err
		}
		eventID := played.PlayedAt.UTC().Format(time.RFC3339Nano) + ":" + played.Track.ID
		tag, err := tx.Exec(ctx, `INSERT INTO listening_events(id,session_hash,provider,provider_event_id,played_at,track_id,provider_track_id,name,artists,album,duration_ms,isrc,raw)
			VALUES($1,$2,'spotify',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(session_hash,provider,provider_event_id) DO NOTHING`, rand.Text(), hash, eventID, played.PlayedAt, trackID, played.Track.ID, played.Track.Name, artistsJSON, played.Track.Album, played.Track.DurationMS, played.Track.ISRC, raw)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			count++
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE listening_imports SET state='completed',error_code='',imported_count=$2,completed_at=now() WHERE id=$1", id, count); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type listeningWorker struct {
	river.WorkerDefaults[ListeningImportArgs]
	importer *ListeningImporter
}

func (w *listeningWorker) Timeout(*river.Job[ListeningImportArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *listeningWorker) Work(ctx context.Context, job *river.Job[ListeningImportArgs]) error {
	err := w.importer.Run(ctx, job.Args.ImportID)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrListeningImportNotFound) || errors.Is(err, ErrNoConnection) {
		return river.JobCancel(err)
	}
	code := "spotify_listening_import_failed"
	var providerErr *ProviderError
	if errors.As(err, &providerErr) && (providerErr.Reconnect || providerErr.Status == 401 || providerErr.Status == 403) {
		code = "reconnect_spotify"
		_, _ = w.importer.pool.Exec(ctx, "UPDATE listening_imports SET state='failed',error_code=$2 WHERE id=$1", job.Args.ImportID, code)
		return river.JobCancel(err)
	}
	_, _ = w.importer.pool.Exec(ctx, "UPDATE listening_imports SET state='failed',error_code=$2 WHERE id=$1", job.Args.ImportID, code)
	return err
}
