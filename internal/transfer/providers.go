package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/applemusic"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/spotify"
)

type SpotifyProvider struct {
	client *spotify.Client
	store  spotify.Store
}

func NewSpotifyProvider(client *spotify.Client, store spotify.Store) *SpotifyProvider {
	return &SpotifyProvider{client: client, store: store}
}

func (p *SpotifyProvider) Name() string { return ProviderSpotify }

func (p *SpotifyProvider) ListPlaylists(ctx context.Context, hash string, offset, limit int) (PlaylistPage, error) {
	page, err := p.client.Playlists(ctx, p.store, hash, offset, limit)
	if err != nil {
		return PlaylistPage{}, mapSpotifyError(err)
	}
	items := make([]Playlist, 0, len(page.Items))
	for _, playlist := range page.Items {
		items = append(items, Playlist{Provider: ProviderSpotify, ID: playlist.ID, Name: playlist.Name, URL: playlist.ExternalURLs.Spotify, SnapshotID: playlist.SnapshotID})
	}
	return PlaylistPage{Items: items, Total: page.Total, Offset: page.Offset, NextOffset: page.NextOffset}, nil
}

func (p *SpotifyProvider) ReadPlaylistEntries(ctx context.Context, hash, playlistID string, offset, limit int) (EntryPage, error) {
	_, entries, err := p.client.ReadSnapshot(ctx, p.store, hash, playlistID)
	if err != nil {
		return EntryPage{}, mapSpotifyError(err)
	}
	if offset > len(entries) {
		offset = len(entries)
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	result := make([]PlaylistEntry, 0, end-offset)
	for _, entry := range entries[offset:end] {
		result = append(result, fromSpotifyEntry(entry))
	}
	page := EntryPage{Items: result, Total: len(entries), Offset: offset}
	if end < len(entries) {
		page.NextOffset = &end
	}
	return page, nil
}

func (p *SpotifyProvider) SearchTracks(ctx context.Context, hash string, query TrackQuery, limit int) ([]Track, error) {
	if limit < 1 || limit > 50 {
		limit = 10
	}
	searchTerm := strings.TrimSpace(query.Name + " " + strings.Join(query.Artists, " "))
	if searchTerm == "" && query.ISRC != "" {
		searchTerm = "isrc:" + query.ISRC
	}
	if searchTerm == "" {
		return []Track{}, nil
	}
	tracks, err := p.client.SearchTracks(ctx, p.store, hash, searchTerm, limit)
	if err != nil {
		return nil, mapSpotifyError(err)
	}
	result := make([]Track, 0, len(tracks))
	for _, item := range tracks {
		result = append(result, fromSpotifyEntry(item).TrackValue())
	}
	return result, nil
}

func (p *SpotifyProvider) CreatePlaylist(ctx context.Context, hash string, input CreatePlaylistInput) (Playlist, error) {
	playlist, err := p.client.CreatePlaylist(ctx, p.store, hash, spotify.CreatePlaylistInput{Name: input.Name, Description: input.Description})
	if err != nil {
		return Playlist{}, mapSpotifyError(err)
	}
	return Playlist{Provider: ProviderSpotify, ID: playlist.ID, Name: playlist.Name, URL: playlist.ExternalURLs.Spotify, SnapshotID: playlist.SnapshotID}, nil
}

func (p *SpotifyProvider) AddTracksToPlaylist(ctx context.Context, hash, playlistID string, trackIDs []string) error {
	for start := 0; start < len(trackIDs); start += 100 {
		end := start + 100
		if end > len(trackIDs) {
			end = len(trackIDs)
		}
		if err := p.client.AddTracksToPlaylist(ctx, p.store, hash, playlistID, trackIDs[start:end]); err != nil {
			return mapSpotifyError(err)
		}
	}
	return nil
}

type AppleMusicProvider struct {
	client *applemusic.Client
	store  applemusic.Store
}

func NewAppleMusicProvider(client *applemusic.Client, store applemusic.Store) *AppleMusicProvider {
	return &AppleMusicProvider{client: client, store: store}
}

func (p *AppleMusicProvider) Name() string { return ProviderAppleMusic }

func (p *AppleMusicProvider) ListPlaylists(ctx context.Context, hash string, offset, limit int) (PlaylistPage, error) {
	connection, err := p.store.Load(ctx, hash)
	if err != nil {
		return PlaylistPage{}, mapAppleError(err)
	}
	page, err := p.client.Playlists(ctx, connection.UserToken, connection.Storefront, offset, limit)
	if err != nil {
		return PlaylistPage{}, mapAppleError(err)
	}
	result := make([]Playlist, 0, len(page.Data))
	for _, playlist := range page.Data {
		result = append(result, Playlist{Provider: ProviderAppleMusic, ID: playlist.ID, Name: playlist.Attributes.Name, URL: playlist.Attributes.URL})
	}
	response := PlaylistPage{Items: result, Offset: offset, Total: offset + len(result)}
	if page.Next != "" {
		next := offset + len(result)
		response.NextOffset = &next
	}
	return response, nil
}

func (p *AppleMusicProvider) ReadPlaylistEntries(ctx context.Context, hash, playlistID string, offset, limit int) (EntryPage, error) {
	connection, err := p.store.Load(ctx, hash)
	if err != nil {
		return EntryPage{}, mapAppleError(err)
	}
	page, err := p.client.PlaylistSongs(ctx, connection.UserToken, connection.Storefront, playlistID, offset, limit)
	if err != nil {
		return EntryPage{}, mapAppleError(err)
	}
	items := make([]PlaylistEntry, 0, len(page.Data))
	for idx, song := range page.Data {
		track := Track{Provider: ProviderAppleMusic, ID: song.ID, Name: song.Attributes.Name, Artists: []string{song.Attributes.ArtistName}, Album: song.Attributes.AlbumName, DurationMS: song.Attributes.DurationMS, ISRC: song.Attributes.ISRC, URL: song.Attributes.URL,
			ProviderIDs: map[string]string{ProviderAppleMusic: song.ID}}
		items = append(items, PlaylistEntry{Position: offset + idx, Track: &track})
	}
	result := EntryPage{Items: items, Offset: offset, Total: offset + len(items)}
	if page.Next != "" {
		next := offset + len(items)
		result.NextOffset = &next
	}
	return result, nil
}

func (p *AppleMusicProvider) SearchTracks(ctx context.Context, hash string, query TrackQuery, limit int) ([]Track, error) {
	connection, err := p.store.Load(ctx, hash)
	if err != nil {
		return nil, mapAppleError(err)
	}
	term := strings.TrimSpace(query.Name + " " + strings.Join(query.Artists, " "))
	if term == "" && query.ISRC != "" {
		term = query.ISRC
	}
	if term == "" {
		return []Track{}, nil
	}
	songs, err := p.client.SearchSongs(ctx, connection.UserToken, connection.Storefront, term, limit)
	if err != nil {
		return nil, mapAppleError(err)
	}
	result := make([]Track, 0, len(songs))
	for _, song := range songs {
		result = append(result, Track{Provider: ProviderAppleMusic, ID: song.ID, Name: song.Attributes.Name, Artists: []string{song.Attributes.ArtistName}, Album: song.Attributes.AlbumName, DurationMS: song.Attributes.DurationMS, ISRC: song.Attributes.ISRC, URL: song.Attributes.URL, ProviderIDs: map[string]string{ProviderAppleMusic: song.ID}})
	}
	return result, nil
}

func (p *AppleMusicProvider) CreatePlaylist(context.Context, string, CreatePlaylistInput) (Playlist, error) {
	return Playlist{}, ErrUnsupportedOperation
}

func (p *AppleMusicProvider) AddTracksToPlaylist(context.Context, string, string, []string) error {
	return ErrUnsupportedOperation
}

func mapSpotifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, spotify.ErrNoConnection) {
		return ErrPreviewNotFound
	}
	var upstream *spotify.ProviderError
	if errors.As(err, &upstream) && upstream.Status == 429 {
		retry := time.Duration(upstream.RetryAfter) * time.Second
		if retry <= 0 {
			retry = 30 * time.Second
		}
		return &RateLimitError{RetryAfter: retry}
	}
	return err
}

func mapAppleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, applemusic.ErrNoConnection) {
		return ErrPreviewNotFound
	}
	if strings.Contains(err.Error(), "HTTP 429") {
		return &RateLimitError{RetryAfter: 30 * time.Second}
	}
	return fmt.Errorf("apple_music: %w", err)
}

func (entry PlaylistEntry) TrackValue() Track {
	if entry.Track == nil {
		return Track{}
	}
	return *entry.Track
}
