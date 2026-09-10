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
		var trackID, name, artistsJSON, album, isrc string
		var duration int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&entry.Position, &entry.Provider, &entry.ProviderTrackID, &entry.Unavailable, &entry.Unsupported,
			&trackID, &name, &artistsJSON, &album, &duration, &isrc, &createdAt, &updatedAt); err != nil {
			return playlist, nil, err
		}
		if trackID != "" {
			var artists []string
			if err := json.Unmarshal([]byte(artistsJSON), &artists); err != nil {
				return playlist, nil, err
			}
			entry.Track = &Track{ID: trackID, Name: name, Artists: artists, Album: album, DurationMS: duration, ISRC: isrc, CreatedAt: createdAt, UpdatedAt: updatedAt}
		}
		entries = append(entries, entry)
	}
	return playlist, entries, rows.Err()
}
