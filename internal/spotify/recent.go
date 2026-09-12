package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type RecentlyPlayed struct {
	Track    Entry     `json:"track"`
	PlayedAt time.Time `json:"played_at"`
}

type RecentlyPlayedPage struct {
	Items  []RecentlyPlayed `json:"items"`
	After  int64            `json:"after,omitempty"`
	Before int64            `json:"before,omitempty"`
}

// RecentlyPlayed reads a bounded page using the cursor timestamp. It never
// follows Spotify-provided URLs, keeping requests on the configured API host.
func (c *Client) RecentlyPlayed(ctx context.Context, store Store, hash string, after *time.Time, limit int) (RecentlyPlayedPage, error) {
	var result RecentlyPlayedPage
	if limit < 1 || limit > 50 {
		return result, errors.New("invalid_recently_played_limit")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if after != nil {
		query.Set("after", strconv.FormatInt(after.UnixMilli(), 10))
	}
	var response struct {
		Items []struct {
			Track    mediaItem `json:"track"`
			PlayedAt time.Time `json:"played_at"`
		} `json:"items"`
		Cursors struct {
			After  string `json:"after"`
			Before string `json:"before"`
		} `json:"cursors"`
	}
	err := c.withConnection(ctx, store, hash, func(connection *Connection) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/me/player/recently-played?"+query.Encode(), nil)
		if err != nil {
			return errors.New("cannot_prepare_recently_played_request")
		}
		req.Header.Set("Authorization", "Bearer "+connection.Tokens.AccessToken)
		return c.request(req, &response)
	})
	if err != nil {
		return result, err
	}
	result.Items = make([]RecentlyPlayed, 0, len(response.Items))
	for _, item := range response.Items {
		entry := recentEntry(item.Track)
		result.Items = append(result.Items, RecentlyPlayed{Track: entry, PlayedAt: item.PlayedAt})
	}
	result.After, _ = strconv.ParseInt(response.Cursors.After, 10, 64)
	result.Before, _ = strconv.ParseInt(response.Cursors.Before, 10, 64)
	return result, nil
}

func recentEntry(m mediaItem) Entry {
	artists := append([]Artist(nil), m.Artists...)
	entry := Entry{Type: m.Type, ID: m.ID, URI: m.URI, Name: m.Name, Artists: artists,
		Album: m.Album.Name, DurationMS: m.DurationMS, ISRC: m.ExternalIDs.ISRC,
		SpotifyURL: m.ExternalURLs.Spotify, LinkedFromID: m.LinkedFrom.ID, IsLocal: m.IsLocal, IsPlayable: m.IsPlayable}
	entry.Unsupported = m.Type != "track"
	entry.Unavailable = !entry.IsLocal && (entry.ID == "" || (entry.IsPlayable != nil && !*entry.IsPlayable))
	return entry
}
