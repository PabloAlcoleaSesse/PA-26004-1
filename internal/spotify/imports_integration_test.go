package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/jobs"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestPostgresPlaylistPipeline(t *testing.T) {
	ctx, pool := testPostgres(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := platform.MigrateApp(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	mode, calls := "normal", 0
	auth, _, _ := testAuth(t, playlistFixture(t, &mode, &calls))
	auth.client.apiURL = strings.TrimSuffix(auth.client.profileURL, "/me")
	store, err := NewPostgresStore(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	auth.store = store
	hash := seedPlaylistSession(t, store)
	importer := NewImporter(pool, store, auth.client)
	queue, err := jobs.NewClient(pool, logger, 1, importer.Register)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	auth.Register(mux)
	auth.RegisterImports(mux, importer, queue)
	post := func() string {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/spotify/playlists/playlist1/imports", nil)
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "playlist-browser"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatalf("enqueue: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			ID string `json:"import_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.ID
	}
	id := post()
	if again := post(); again != id {
		t.Fatal("duplicate click enqueued another active import")
	}
	var args []byte
	if err := pool.QueryRow(ctx, "SELECT j.args FROM river_job j JOIN spotify_imports i ON i.river_job_id = j.id WHERE i.id = $1", id).Scan(&args); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil || len(payload) != 1 || payload["import_id"] != id {
		t.Fatal("job contains more than an opaque import ID")
	}
	if _, err := importer.Status(ctx, sessionHash("other-browser"), id); !errors.Is(err, ErrImportNotFound) {
		t.Fatal("cross-session import exposed")
	}
	// Start a real River worker against this isolated schema and mock provider.
	workCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := queue.Start(workCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = queue.StopAndCancel(stop)
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		status, err := importer.Status(ctx, hash, id)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not complete: %+v", status)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := queue.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := importer.Snapshot(ctx, hash, id, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Total != 52 || len(snapshot.Items) != 50 || snapshot.NextOffset == nil || *snapshot.NextOffset != 50 {
		t.Fatal("snapshot pagination incorrect")
	}
	if snapshot.Items[0].ID != snapshot.Items[1].ID || !snapshot.Items[2].Unavailable {
		t.Fatal("duplicate/unavailable occurrence lost")
	}
	last, err := importer.Snapshot(ctx, hash, id, 50, 50)
	if err != nil || len(last.Items) != 2 || last.Items[1].Position != 51 || last.NextOffset != nil {
		t.Fatal("last snapshot page incorrect")
	}
	if _, err := importer.Snapshot(ctx, sessionHash("other-browser"), id, 0, 50); !errors.Is(err, ErrImportNotFound) {
		t.Fatal("cross-session snapshot exposed")
	}
	// Simulate a retry after publication but before the queue acknowledgement.
	beforeCalls := calls
	if err := importer.Run(ctx, id); err != nil {
		t.Fatal(err)
	}
	if calls != beforeCalls {
		t.Fatal("completed import fetched Spotify again")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM spotify_snapshot_entries WHERE import_id = $1", id).Scan(&count); err != nil || count != 52 {
		t.Fatal("retry duplicated entries")
	}
	// An edited playlist must leave the previous snapshot intact and publish no
	// partial snapshot for the new request, even after reading all of its pages.
	mode, calls = "changed", 0
	changedID := post()
	worker := &importWorker{importer: importer}
	if err := worker.Work(ctx, &river.Job[ImportArgs]{Args: ImportArgs{ImportID: changedID}}); err == nil {
		t.Fatal("edited playlist accepted")
	}
	if _, err := importer.Snapshot(ctx, hash, changedID, 0, 50); !errors.Is(err, ErrImportNotFound) {
		t.Fatal("partial snapshot published")
	}
	status, err := importer.Status(ctx, hash, changedID)
	if err != nil || status.ErrorCode != "playlist_changed_during_import" {
		t.Fatal("safe failure code not saved")
	}
	if err := store.Delete(ctx, hash); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"spotify_imports", "spotify_snapshots", "spotify_snapshot_entries"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("disconnect left data in %s", table)
		}
	}
	if err := importer.Run(ctx, changedID); !errors.Is(err, ErrNoConnection) {
		t.Fatal("disconnected session can import")
	}
}
