package spotify

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

func (c *Client) SearchTracks(ctx context.Context, store Store, hash, query string, limit int) ([]Entry, error) {
	if query == "" || limit < 1 || limit > 50 {
		return nil, errors.New("invalid_search_request")
	}
	var body struct {
		Tracks struct {
			Items []mediaItem `json:"items"`
		} `json:"tracks"`
	}
	err := c.playlistGET(ctx, store, hash, "/search", url.Values{"q": {query}, "type": {"track"}, "limit": {strconv.Itoa(limit)}}, &body)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(body.Tracks.Items))
	for idx, track := range body.Tracks.Items {
		occurrence := playlistOccurrence{Item: &track}
		entries = append(entries, occurrence.entry(idx))
	}
	return entries, nil
}
