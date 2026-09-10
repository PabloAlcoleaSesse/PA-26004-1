package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxSnapshotItems = 10000

type CreatePlaylistInput struct {
	Name        string
	Description string
}

var ErrPlaylistChanged = errors.New("playlist_changed_during_import")
var ErrPlaylistTooLarge = errors.New("playlist_exceeds_import_limit")

// Playlist holds only the metadata needed for discovery and versioned imports.
// Both summary fields are decoded because older Spotify apps may receive tracks.
type Playlist struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SnapshotID   string `json:"snapshot_id"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Items *struct {
		Total int `json:"total"`
	} `json:"items,omitempty"`
	Tracks *struct {
		Total int `json:"total"`
	} `json:"tracks,omitempty"`
}

func (p Playlist) count() (int, error) {
	if p.Items != nil {
		return p.Items.Total, nil
	}
	if p.Tracks != nil {
		return p.Tracks.Total, nil
	}
	return 0, errors.New("missing_playlist_item_count")
}

// Page exposes an offset, never a provider-controlled URL for callers to follow.
type Page[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset"`
}

type providerPage[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
}

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Entry is a playlist occurrence, not a deduplicated track. Null entries and
// local files retain their original positions for accurate later transfers.
type Entry struct {
	Position     int      `json:"position"`
	AddedAt      *string  `json:"added_at"`
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	URI          string   `json:"uri"`
	Name         string   `json:"name"`
	Artists      []Artist `json:"artists"`
	Album        string   `json:"album"`
	DurationMS   int      `json:"duration_ms"`
	ISRC         string   `json:"isrc"`
	SpotifyURL   string   `json:"spotify_url"`
	LinkedFromID string   `json:"linked_from_id,omitempty"`
	IsLocal      bool     `json:"is_local"`
	Unavailable  bool     `json:"unavailable"`
	Unsupported  bool     `json:"unsupported"`
	IsPlayable   *bool    `json:"is_playable"`
}

type mediaItem struct {
	Type    string   `json:"type"`
	ID      string   `json:"id"`
	URI     string   `json:"uri"`
	Name    string   `json:"name"`
	Artists []Artist `json:"artists"`
	Album   struct {
		Name string `json:"name"`
	} `json:"album"`
	DurationMS  int `json:"duration_ms"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	LinkedFrom struct {
		ID string `json:"id"`
	} `json:"linked_from"`
	IsLocal    bool  `json:"is_local"`
	IsPlayable *bool `json:"is_playable"`
}

type playlistOccurrence struct {
	AddedAt *string    `json:"added_at"`
	IsLocal bool       `json:"is_local"`
	Item    *mediaItem `json:"item"`
	Track   *mediaItem `json:"track"`
}

func (o playlistOccurrence) entry(position int) Entry {
	e := Entry{Position: position, AddedAt: o.AddedAt, IsLocal: o.IsLocal, Artists: []Artist{}}
	m := o.Item
	if m == nil {
		m = o.Track
	}
	if m == nil {
		e.Unavailable = true
		return e
	}
	e.Type, e.ID, e.URI, e.Name = m.Type, m.ID, m.URI, m.Name
	if m.Artists != nil {
		e.Artists = m.Artists
	}
	e.Album, e.DurationMS, e.ISRC = m.Album.Name, m.DurationMS, m.ExternalIDs.ISRC
	e.SpotifyURL, e.LinkedFromID = m.ExternalURLs.Spotify, m.LinkedFrom.ID
	e.IsLocal, e.IsPlayable = e.IsLocal || m.IsLocal, m.IsPlayable
	e.Unsupported = m.Type != "track"
	e.Unavailable = !e.IsLocal && (m.ID == "" || (m.IsPlayable != nil && !*m.IsPlayable))
	return e
}

func validPlaylistID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

// withConnection commits refreshed credentials even if the resource request
// fails. It locks only one page request at a time, not an entire library import.
func (c *Client) withConnection(ctx context.Context, store Store, hash string, call func(*Connection) error) error {
	var resourceErr error
	err := store.Update(ctx, hash, func(connection *Connection) error {
		refreshed := false
		if !connection.Tokens.ExpiresAt.After(time.Now().Add(time.Minute)) {
			tokens, err := c.Refresh(ctx, connection.Tokens)
			if err != nil {
				return err
			}
			connection.Tokens = tokens
			refreshed = true
		}
		resourceErr = call(connection)
		var upstream *ProviderError
		if !refreshed && errors.As(resourceErr, &upstream) && upstream.Status == http.StatusUnauthorized {
			tokens, err := c.Refresh(ctx, connection.Tokens)
			if err != nil {
				return err
			}
			connection.Tokens = tokens
			resourceErr = call(connection)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return resourceErr
}

func (c *Client) playlistGET(ctx context.Context, store Store, hash, path string, q url.Values, dst any) error {
	// Always build requests from our fixed API base. Spotify's next/href fields
	// are not followed, preventing bearer-token leakage via hostile pagination.
	return c.withConnection(ctx, store, hash, func(connection *Connection) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return errors.New("cannot_prepare_playlist_request")
		}
		req.Header.Set("Authorization", "Bearer "+connection.Tokens.AccessToken)
		return c.request(req, dst)
	})
}

func (c *Client) Playlists(ctx context.Context, store Store, hash string, offset, limit int) (Page[Playlist], error) {
	var result Page[Playlist]
	if offset < 0 || offset > 100000 || limit < 1 || limit > 50 {
		return result, errors.New("invalid_pagination")
	}
	var page providerPage[Playlist]
	err := c.playlistGET(ctx, store, hash, "/me/playlists", url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}, &page)
	if err != nil {
		return result, err
	}
	if page.Offset != offset || page.Total < 0 || len(page.Items) > limit || (len(page.Items) == 0 && offset < page.Total) {
		return result, errors.New("invalid_playlist_page")
	}
	if page.Items == nil {
		page.Items = []Playlist{}
	}
	result = Page[Playlist]{Items: page.Items, Offset: offset, Total: page.Total}
	if next := offset + len(page.Items); next < page.Total {
		result.NextOffset = &next
	}
	return result, nil
}

func (c *Client) playlist(ctx context.Context, store Store, hash, id string) (Playlist, error) {
	var p Playlist
	if !validPlaylistID(id) {
		return p, errors.New("invalid_playlist_id")
	}
	if err := c.playlistGET(ctx, store, hash, "/playlists/"+id, url.Values{}, &p); err != nil {
		return p, err
	}
	if p.ID != id || p.SnapshotID == "" {
		return p, errors.New("invalid_playlist_metadata")
	}
	return p, nil
}

// ReadSnapshot publishes nothing until every page has been read and the
// playlist's version is confirmed unchanged. A worker retry starts over safely.
func (c *Client) ReadSnapshot(ctx context.Context, store Store, hash, id string) (Playlist, []Entry, error) {
	before, err := c.playlist(ctx, store, hash, id)
	if err != nil {
		return before, nil, err
	}
	total, err := before.count()
	if err != nil || total < 0 {
		return before, nil, errors.New("invalid_playlist_item_count")
	}
	if total > maxSnapshotItems {
		return before, nil, ErrPlaylistTooLarge
	}
	entries := make([]Entry, 0, total)
	for offset := 0; offset < total; {
		var page providerPage[playlistOccurrence]
		q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {"50"}, "additional_types": {"track,episode"}}
		if err := c.playlistGET(ctx, store, hash, "/playlists/"+id+"/items", q, &page); err != nil {
			return before, nil, err
		}
		if page.Total != total {
			return before, nil, ErrPlaylistChanged
		}
		if page.Offset != offset || len(page.Items) == 0 || len(page.Items) > 50 || offset+len(page.Items) > total {
			return before, nil, fmt.Errorf("invalid_playlist_items_page")
		}
		for _, item := range page.Items {
			entries = append(entries, item.entry(len(entries)))
		}
		offset += len(page.Items)
	}
	after, err := c.playlist(ctx, store, hash, id)
	if err != nil {
		return before, nil, err
	}
	afterTotal, err := after.count()
	if err != nil || after.SnapshotID != before.SnapshotID || afterTotal != total {
		return before, nil, ErrPlaylistChanged
	}
	return before, entries, nil
}

// CreatePlaylist creates a private playlist for the connected account. The
// current Spotify API uses /me/playlists; no user ID is accepted from a request.
func (c *Client) CreatePlaylist(ctx context.Context, store Store, hash string, input CreatePlaylistInput) (Playlist, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 100 || len(input.Description) > 300 {
		return Playlist{}, errors.New("invalid_playlist_input")
	}
	var created Playlist
	err := c.withConnection(ctx, store, hash, func(connection *Connection) error {
		body, err := json.Marshal(struct {
			Name          string `json:"name"`
			Public        bool   `json:"public"`
			Collaborative bool   `json:"collaborative"`
			Description   string `json:"description,omitempty"`
		}{input.Name, false, false, input.Description})
		if err != nil {
			return errors.New("cannot encode playlist request")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/me/playlists", strings.NewReader(string(body)))
		if err != nil {
			return errors.New("cannot prepare playlist create request")
		}
		req.Header.Set("Authorization", "Bearer "+connection.Tokens.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		return c.requestWithStatus(req, &created, http.StatusCreated, http.StatusOK)
	})
	if err != nil {
		return Playlist{}, err
	}
	if created.ID == "" || !validPlaylistID(created.ID) {
		return Playlist{}, errors.New("invalid_created_playlist")
	}
	return created, nil
}

// AddTracksToPlaylist appends up to 100 Spotify URIs in one request. Callers
// use small ordered batches so a retry can reconcile each position safely.
func (c *Client) AddTracksToPlaylist(ctx context.Context, store Store, hash, playlistID string, trackIDs []string) error {
	if !validPlaylistID(playlistID) || len(trackIDs) == 0 || len(trackIDs) > 100 {
		return errors.New("invalid_playlist_tracks_request")
	}
	uris := make([]string, len(trackIDs))
	for i, id := range trackIDs {
		if !validPlaylistID(id) {
			return errors.New("invalid_track_id")
		}
		uris[i] = "spotify:track:" + id
	}
	body, err := json.Marshal(struct {
		URIs []string `json:"uris"`
	}{uris})
	if err != nil {
		return errors.New("cannot encode playlist tracks request")
	}
	return c.withConnection(ctx, store, hash, func(connection *Connection) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/playlists/"+url.PathEscape(playlistID)+"/items", strings.NewReader(string(body)))
		if err != nil {
			return errors.New("cannot prepare playlist tracks request")
		}
		req.Header.Set("Authorization", "Bearer "+connection.Tokens.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		return c.requestWithStatus(req, nil, http.StatusCreated, http.StatusOK, http.StatusNoContent)
	})
}
