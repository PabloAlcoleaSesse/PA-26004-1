package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPlaylistNotFound = errors.New("library_playlist_not_found")
var ErrInvalidTasteLimit = errors.New("invalid_taste_limit")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func TrackID(provider, providerID string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + providerID))
	return hex.EncodeToString(sum[:])
}

func (s *Store) ListPlaylists(ctx context.Context, sessionHash string, limit int) ([]Playlist, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("invalid_library_limit")
	}
	rows, err := s.pool.Query(ctx, `SELECT id,source_provider,source_playlist_id,name,url,updated_at
		FROM library_playlists WHERE session_hash = $1 ORDER BY updated_at DESC LIMIT $2`, sessionHash, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	playlists := []Playlist{}
	for rows.Next() {
		var playlist Playlist
		if err := rows.Scan(&playlist.ID, &playlist.SourceProvider, &playlist.SourceID, &playlist.Name, &playlist.URL, &playlist.UpdatedAt); err != nil {
			return nil, err
		}
		playlists = append(playlists, playlist)
	}
	return playlists, rows.Err()
}

func (s *Store) GetPlaylist(ctx context.Context, sessionHash, id string, offset, limit int) (Playlist, []PlaylistEntry, error) {
	if offset < 0 || limit < 1 || limit > 500 {
		return Playlist{}, nil, errors.New("invalid_library_pagination")
	}
	var playlist Playlist
	err := s.pool.QueryRow(ctx, `SELECT id,source_provider,source_playlist_id,name,url,updated_at
		FROM library_playlists WHERE id = $1 AND session_hash = $2`, id, sessionHash).
		Scan(&playlist.ID, &playlist.SourceProvider, &playlist.SourceID, &playlist.Name, &playlist.URL, &playlist.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Playlist{}, nil, ErrPlaylistNotFound
	}
	if err != nil {
		return Playlist{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.position,e.provider,e.provider_track_id,e.unavailable,e.unsupported,
			COALESCE(t.id,''),COALESCE(t.name,''),COALESCE(t.artists,'[]'::jsonb),COALESCE(t.album,''),
			COALESCE(t.duration_ms,0),COALESCE(t.isrc,''),COALESCE(t.created_at,'epoch'::timestamptz),COALESCE(t.updated_at,'epoch'::timestamptz)
		FROM library_playlist_entries e LEFT JOIN library_tracks t ON t.id = e.track_id
		WHERE e.playlist_id = $1 AND e.position >= $2 ORDER BY e.position LIMIT $3`, id, offset, limit)
	if err != nil {
		return playlist, nil, err
	}
	defer rows.Close()
	entries := []PlaylistEntry{}
	for rows.Next() {
		var entry PlaylistEntry
		var trackID, name, album, isrc string
		var artistsJSON []byte
		var duration int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&entry.Position, &entry.Provider, &entry.ProviderTrackID, &entry.Unavailable, &entry.Unsupported,
			&trackID, &name, &artistsJSON, &album, &duration, &isrc, &createdAt, &updatedAt); err != nil {
			return playlist, nil, err
		}
		if trackID != "" {
			var artists []string
			if err := json.Unmarshal(artistsJSON, &artists); err != nil {
				return playlist, nil, err
			}
			entry.Track = &Track{ID: trackID, Name: name, Artists: artists, Album: album, DurationMS: duration, ISRC: isrc, CreatedAt: createdAt, UpdatedAt: updatedAt}
		}
		entries = append(entries, entry)
	}
	return playlist, entries, rows.Err()
}

func (s *Store) Stats(ctx context.Context, sessionHash string) (Stats, error) {
	var stats Stats
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM library_playlists WHERE session_hash=$1),
		(SELECT count(*) FROM library_playlist_entries e JOIN library_playlists p ON p.id=e.playlist_id WHERE p.session_hash=$1),
		(SELECT count(DISTINCT e.track_id) FROM library_playlist_entries e JOIN library_playlists p ON p.id=e.playlist_id WHERE p.session_hash=$1 AND e.track_id IS NOT NULL),
		(SELECT count(*) FROM library_playlist_entries e JOIN library_playlists p ON p.id=e.playlist_id WHERE p.session_hash=$1 AND e.unavailable),
		(SELECT count(*) FROM library_playlist_entries e JOIN library_playlists p ON p.id=e.playlist_id WHERE p.session_hash=$1 AND e.unsupported)`, sessionHash).
		Scan(&stats.Playlists, &stats.Entries, &stats.Tracks, &stats.Unavailable, &stats.Unsupported)
	if err != nil {
		return Stats{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT source_provider,count(*) FROM library_playlists WHERE session_hash=$1 GROUP BY source_provider ORDER BY source_provider`, sessionHash)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	stats.ByProvider = map[string]int{}
	for rows.Next() {
		var provider string
		var count int
		if err := rows.Scan(&provider, &count); err != nil {
			return Stats{}, err
		}
		stats.ByProvider[provider] = count
	}
	return stats, rows.Err()
}

func (s *Store) Taste(ctx context.Context, sessionHash string, limit int) (TasteSummary, error) {
	if limit < 1 || limit > 50 {
		return TasteSummary{}, ErrInvalidTasteLimit
	}
	summary := TasteSummary{TopTracks: []TasteItem{}, TopArtists: []TasteItem{}}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM listening_events WHERE session_hash=$1", sessionHash).Scan(&summary.TotalPlays); err != nil {
		return TasteSummary{}, err
	}
	trackRows, err := s.pool.Query(ctx, `SELECT name,count(*) FROM listening_events WHERE session_hash=$1 AND name<>'' GROUP BY name ORDER BY count(*) DESC,name LIMIT $2`, sessionHash, limit)
	if err != nil {
		return TasteSummary{}, err
	}
	defer trackRows.Close()
	for trackRows.Next() {
		var item TasteItem
		if err := trackRows.Scan(&item.Name, &item.Plays); err != nil {
			return TasteSummary{}, err
		}
		summary.TopTracks = append(summary.TopTracks, item)
	}
	if err := trackRows.Err(); err != nil {
		return TasteSummary{}, err
	}
	artistRows, err := s.pool.Query(ctx, `SELECT artist,count(*) FROM listening_events e CROSS JOIN LATERAL jsonb_array_elements_text(e.artists) AS artist
		WHERE e.session_hash=$1 AND artist<>'' GROUP BY artist ORDER BY count(*) DESC,artist LIMIT $2`, sessionHash, limit)
	if err != nil {
		return TasteSummary{}, err
	}
	defer artistRows.Close()
	for artistRows.Next() {
		var item TasteItem
		if err := artistRows.Scan(&item.Name, &item.Plays); err != nil {
			return TasteSummary{}, err
		}
		summary.TopArtists = append(summary.TopArtists, item)
	}
	return summary, artistRows.Err()
}
