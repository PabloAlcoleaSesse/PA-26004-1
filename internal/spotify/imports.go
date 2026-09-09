package spotify

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

var ErrImportNotFound = errors.New("import_not_found")
var ErrTooManyImports = errors.New("too_many_active_imports")

// ImportArgs carries only an opaque request ID. Credentials, cookies, and
// session hashes never enter River's job payloads or logs.
type ImportArgs struct {
	ImportID string `json:"import_id"`
}

func (ImportArgs) Kind() string { return "spotify_playlist_import" }

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

type ImportStatus struct {
	ID         string `json:"id"`
	PlaylistID string `json:"playlist_id"`
	State      string `json:"state"`
	ErrorCode  string `json:"error_code,omitempty"`
}

type Snapshot struct {
	ImportID   string    `json:"import_id"`
	PlaylistID string    `json:"playlist_id"`
	SnapshotID string    `json:"snapshot_id"`
	Name       string    `json:"name"`
	SpotifyURL string    `json:"spotify_url"`
	Total      int       `json:"total"`
	CreatedAt  time.Time `json:"created_at"`
	Offset     int       `json:"offset"`
	NextOffset *int      `json:"next_offset"`
	Items      []Entry   `json:"items"`
}

// Enqueue locks the connection, checks ownership, and inserts the request and
// River job in one transaction. Repeated clicks reuse an already-active import.
func (i *Importer) Enqueue(ctx context.Context, queue *river.Client[pgx.Tx], hash, playlistID string) (string, error) {
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var owner string
	err = tx.QueryRow(ctx, "SELECT session_hash FROM spotify_connections WHERE session_hash = $1 AND expires_at > now() FOR UPDATE", hash).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoConnection
	}
	if err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT i.id FROM spotify_imports i JOIN river_job j ON j.id = i.river_job_id
		WHERE i.session_hash = $1 AND i.playlist_id = $2 AND j.state IN ('available','pending','running','retryable','scheduled') LIMIT 1`, hash, playlistID).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM spotify_imports i JOIN river_job j ON j.id = i.river_job_id
		WHERE i.session_hash = $1 AND j.state IN ('available','pending','running','retryable','scheduled')`, hash).Scan(&active); err != nil {
		return "", err
	}
	if active >= 10 {
		return "", ErrTooManyImports
	}
	id := rand.Text()
	job, err := queue.InsertTx(ctx, tx, ImportArgs{ImportID: id}, &river.InsertOpts{MaxAttempts: 5})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO spotify_imports (id, session_hash, playlist_id, river_job_id) VALUES ($1,$2,$3,$4)", id, hash, playlistID, job.Job.ID); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (i *Importer) Status(ctx context.Context, hash, id string) (ImportStatus, error) {
	var result ImportStatus
	// Snapshot existence is authoritative even if River has purged an old job.
	err := i.pool.QueryRow(ctx, `SELECT i.id, i.playlist_id,
		CASE WHEN s.import_id IS NOT NULL THEN 'completed' ELSE COALESCE(j.state::text, 'expired') END, i.error_code
		FROM spotify_imports i JOIN spotify_connections c ON c.session_hash = i.session_hash
		LEFT JOIN river_job j ON j.id = i.river_job_id LEFT JOIN spotify_snapshots s ON s.import_id = i.id
		WHERE i.id = $1 AND i.session_hash = $2 AND c.expires_at > now()`, id, hash).Scan(&result.ID, &result.PlaylistID, &result.State, &result.ErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrImportNotFound
	}
	return result, err
}

func (i *Importer) Snapshot(ctx context.Context, hash, id string, offset, limit int) (Snapshot, error) {
	var result Snapshot
	// A single transaction gives metadata and entries the same view even if
	// the user disconnects concurrently. New requests then lose access.
	tx, err := i.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `SELECT s.import_id,s.playlist_id,s.snapshot_id,s.name,s.spotify_url,s.item_count,s.created_at
		FROM spotify_snapshots s JOIN spotify_imports i ON i.id = s.import_id
		JOIN spotify_connections c ON c.session_hash = i.session_hash
		WHERE s.import_id = $1 AND i.session_hash = $2 AND c.expires_at > now()`, id, hash).Scan(&result.ImportID, &result.PlaylistID, &result.SnapshotID, &result.Name, &result.SpotifyURL, &result.Total, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrImportNotFound
	}
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, "SELECT item FROM spotify_snapshot_entries WHERE import_id = $1 AND position >= $2 ORDER BY position LIMIT $3", id, offset, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Items = []Entry{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return result, err
		}
		var entry Entry
		if err := json.Unmarshal(body, &entry); err != nil {
			return result, err
		}
		result.Items = append(result.Items, entry)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Offset = offset
	if next := offset + len(result.Items); next < result.Total {
		result.NextOffset = &next
	}
	return result, tx.Commit(ctx)
}

func (i *Importer) Run(ctx context.Context, id string) error {
	var hash, playlistID string
	var complete bool
	err := i.pool.QueryRow(ctx, `SELECT i.session_hash, i.playlist_id, EXISTS(SELECT 1 FROM spotify_snapshots WHERE import_id = i.id)
		FROM spotify_imports i JOIN spotify_connections c ON c.session_hash = i.session_hash
		WHERE i.id = $1 AND c.expires_at > now()`, id).Scan(&hash, &playlistID, &complete)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoConnection
	}
	if err != nil {
		return errors.New("import_database_unavailable")
	}
	if complete {
		return nil
	} // A retry after commit must not create duplicates.
	playlist, entries, err := i.client.ReadSnapshot(ctx, i.store, hash, playlistID)
	if err != nil {
		return err
	}
	if err := i.saveSnapshot(ctx, id, hash, playlist, entries); err != nil {
		if errors.Is(err, ErrNoConnection) {
			return err
		}
		return errors.New("snapshot_save_failed")
	}
	return nil
}

func (i *Importer) saveSnapshot(ctx context.Context, id, hash string, playlist Playlist, entries []Entry) error {
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var owner string
	err = tx.QueryRow(ctx, "SELECT session_hash FROM spotify_connections WHERE session_hash = $1 AND expires_at > now() FOR UPDATE", hash).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoConnection
	}
	if err != nil {
		return err
	}
	// The unique import ID makes publication idempotent, including concurrent
	// deliveries. A partial snapshot is never visible outside this transaction.
	tag, err := tx.Exec(ctx, `INSERT INTO spotify_snapshots (import_id,playlist_id,snapshot_id,name,spotify_url,item_count)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (import_id) DO NOTHING`, id, playlist.ID, playlist.SnapshotID, playlist.Name, playlist.ExternalURLs.Spotify, len(entries))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	batch := &pgx.Batch{}
	for _, entry := range entries {
		body, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		batch.Queue("INSERT INTO spotify_snapshot_entries (import_id,position,item) VALUES ($1,$2,$3)", id, entry.Position, body)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE spotify_imports SET error_code = '' WHERE id = $1", id); err != nil {
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
	code, permanent := "import_failed", false
	var upstream *ProviderError
	if errors.As(err, &upstream) {
		switch {
		case upstream.Reconnect || upstream.Status == 401:
			code, permanent = "reconnect_spotify", true
		case upstream.Status == 403:
			code, permanent = "playlist_access_denied", true
		case upstream.Status == 404:
			code, permanent = "playlist_not_found", true
		case upstream.Status == 429:
			code = "spotify_rate_limited"
		}
	}
	if errors.Is(err, ErrNoConnection) {
		code, permanent = "connection_expired_or_removed", true
	}
	if errors.Is(err, ErrPlaylistTooLarge) {
		code, permanent = "playlist_exceeds_import_limit", true
	}
	if errors.Is(err, ErrPlaylistChanged) {
		code = "playlist_changed_during_import"
	}
	// Persist only controlled error codes, never SQL errors or provider bodies.
	_, _ = w.importer.pool.Exec(ctx, "UPDATE spotify_imports SET error_code = $2 WHERE id = $1", job.Args.ImportID, code)
	if permanent {
		return river.JobCancel(errors.New(code))
	}
	if upstream != nil && upstream.Status == 429 {
		delay := upstream.RetryAfter
		if delay < 1 {
			delay = 30
		}
		return river.JobSnooze(time.Duration(delay) * time.Second)
	}
	return errors.New(code)
}
