package applemusic

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *Client) SearchSongs(ctx context.Context, userToken, storefront, term string, limit int) ([]Song, error) {
	if storefront == "" || term == "" || limit < 1 || limit > 25 {
		return nil, errors.New("invalid Apple Music search request")
	}
	var body struct {
		Results struct {
			Songs struct {
				Data []Song `json:"data"`
			} `json:"songs"`
		} `json:"results"`
	}
	if err := c.request(ctx, userToken, "/catalog/"+url.PathEscape(storefront)+"/search", url.Values{
		"term":  {term},
		"types": {"songs"},
		"limit": {fmt.Sprint(limit)},
		"l":     {storefront},
	}, &body); err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return []Song{}, nil
		}
		return nil, err
	}
	if body.Results.Songs.Data == nil {
		return []Song{}, nil
	}
	return body.Results.Songs.Data, nil
}
