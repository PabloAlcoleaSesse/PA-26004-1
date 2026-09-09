// Package applemusic implements Apple Music API requests using a MusicKit user token.
package applemusic

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Client struct {
	teamID, keyID string
	privateKey    *ecdsa.PrivateKey
	baseURL       string
	http          *http.Client
}

type Profile struct {
	ID string `json:"id"`
}
type Playlist struct {
	ID         string `json:"id"`
	Attributes struct {
		Name        string `json:"name"`
		Description struct {
			Standard string `json:"standard"`
		} `json:"description"`
		URL string `json:"url"`
	} `json:"attributes"`
}
type Song struct {
	ID         string `json:"id"`
	Attributes struct {
		Name       string `json:"name"`
		ArtistName string `json:"artistName"`
		AlbumName  string `json:"albumName"`
		DurationMS int    `json:"durationInMillis"`
		ISRC       string `json:"isrc"`
		URL        string `json:"url"`
	} `json:"attributes"`
}
type Page[T any] struct {
	Data []T    `json:"data"`
	Next string `json:"next"`
}

func NewClient(teamID, keyID, privateKeyPEM string) (*Client, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("APPLE_PRIVATE_KEY is not PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("APPLE_PRIVATE_KEY is not a PKCS#8 key")
	}
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok || ecdsaKey.Curve.Params().Name != "P-256" {
		return nil, errors.New("APPLE_PRIVATE_KEY must be an ES256 P-256 key")
	}
	return &Client{teamID: teamID, keyID: keyID, privateKey: ecdsaKey, baseURL: "https://api.music.apple.com/v1", http: &http.Client{Timeout: 8 * time.Second}}, nil
}

func (c *Client) DeveloperToken(now time.Time) (string, error) {
	// Apple accepts ES256 JWTs with issuer=Team ID and key ID in the header.
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"iss": c.teamID, "iat": now.Unix(), "exp": now.Add(180 * 24 * time.Hour).Unix()})
	token.Header["kid"] = c.keyID
	return token.SignedString(c.privateKey)
}

func (c *Client) request(ctx context.Context, userToken, path string, query url.Values, destination any) error {
	developerToken, err := c.DeveloperToken(time.Now())
	if err != nil {
		return errors.New("could not sign Apple developer token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return errors.New("could not prepare Apple Music request")
	}
	req.Header.Set("Authorization", "Bearer "+developerToken)
	req.Header.Set("Music-User-Token", userToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("Apple Music request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Apple Music request failed (HTTP %d)", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(destination); err != nil {
		return errors.New("invalid Apple Music response")
	}
	return nil
}

func (c *Client) Playlists(ctx context.Context, userToken, storefront string, offset, limit int) (Page[Playlist], error) {
	var result Page[Playlist]
	if storefront == "" || len(storefront) > 16 || strings.ContainsAny(storefront, "/?&") || offset < 0 || limit < 1 || limit > 100 {
		return result, errors.New("invalid Apple Music playlist pagination")
	}
	var page struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"attributes"`
		} `json:"data"`
		Next string `json:"next"`
	}
	if err := c.request(ctx, userToken, "/me/library/playlists", url.Values{"offset": {fmt.Sprint(offset)}, "limit": {fmt.Sprint(limit)}, "l": {storefront}}, &page); err != nil {
		return result, err
	}
	result.Data = make([]Playlist, len(page.Data))
	for n, p := range page.Data {
		result.Data[n] = Playlist{ID: p.ID}
		result.Data[n].Attributes.Name = p.Attributes.Name
		result.Data[n].Attributes.URL = p.Attributes.URL
	}
	result.Next = page.Next
	return result, nil
}

func (c *Client) PlaylistSongs(ctx context.Context, userToken, storefront, playlistID string, offset, limit int) (Page[Song], error) {
	var result Page[Song]
	if storefront == "" || len(playlistID) == 0 || len(playlistID) > 128 || strings.ContainsAny(playlistID, "/?&") || offset < 0 || limit < 1 || limit > 100 {
		return result, errors.New("invalid Apple Music playlist request")
	}
	var page Page[Song]
	if err := c.request(ctx, userToken, "/me/library/playlists/"+url.PathEscape(playlistID)+"/tracks", url.Values{"offset": {fmt.Sprint(offset)}, "limit": {fmt.Sprint(limit)}, "l": {storefront}}, &page); err != nil {
		return result, err
	}
	return page, nil
}
