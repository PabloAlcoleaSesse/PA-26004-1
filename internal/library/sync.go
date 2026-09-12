package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

type SnapshotEntry struct {
	Provider    string
	Position    int
	ProviderID  string
	Name        string
	Artists     []string
	Album       string
	DurationMS  int
	ISRC        string
	URL         string
	Unavailable bool
	Unsupported bool
	Raw         any
}

func playlistID(sessionHash, provider, sourceID string) string {
	sum := sha256.Sum256([]byte(sessionHash + "\x00" + provider + "\x00" + sourceID))
	return hex.EncodeToString(sum[:])
}

// SyncSpotifySnapshotTx publishes a completed provider snapshot into the
// provider-neutral catalog in the same transaction as its source snapshot.
func SyncSpotifySnapshotTx(ctx context.Context, tx pgx.Tx, sessionHash, sourceID, name, url string, entries []SnapshotEntry) error {
	return SyncPlaylistTx(ctx, tx, sessionHash, "spotify", sourceID, name, url, entries)
}

// SyncPlaylistTx replaces one provider playlist projection atomically. Entries
// retain their source positions, including unavailable and unsupported items.
func SyncPlaylistTx(ctx context.Context, tx pgx.Tx, sessionHash, provider, sourceID, name, url string, entries []SnapshotEntry) error {
	id := playlistID(sessionHash, provider, sourceID)
	if _, err := tx.Exec(ctx, `INSERT INTO library_playlists (id,session_hash,source_provider,source_playlist_id,name,url,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (session_hash,source_provider,source_playlist_id)
		DO UPDATE SET name = excluded.name, url = excluded.url, updated_at = now()`, id, sessionHash, provider, sourceID, name, url); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM library_playlist_entries WHERE playlist_id = $1", id); err != nil {
		return err
	}
	for _, entry := range entries {
		trackID := ""
		if entry.ProviderID != "" && !entry.Unavailable && !entry.Unsupported {
			trackID = TrackID(provider, entry.ProviderID)
			artists, err := json.Marshal(entry.Artists)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO library_tracks (id,name,artists,album,duration_ms,isrc,updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,now())
				ON CONFLICT (id) DO UPDATE SET name = excluded.name, artists = excluded.artists, album = excluded.album,
				 duration_ms = excluded.duration_ms, isrc = excluded.isrc, updated_at = now()`, trackID, entry.Name, artists, entry.Album, entry.DurationMS, entry.ISRC); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO library_track_sources (track_id,provider,provider_id,url)
				VALUES ($1,$2,$3,$4) ON CONFLICT (provider,provider_id)
				DO UPDATE SET track_id = excluded.track_id, url = excluded.url`, trackID, provider, entry.ProviderID, entry.URL); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(entry.Raw)
		if err != nil {
			return err
		}
		if string(raw) == "null" {
			raw = []byte(`{}`)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO library_playlist_entries (playlist_id,position,track_id,provider,provider_track_id,unavailable,unsupported,raw)
			VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8)`, id, entry.Position, trackID, provider, entry.ProviderID, entry.Unavailable, entry.Unsupported, raw); err != nil {
			return err
		}
	}
	return nil
}
