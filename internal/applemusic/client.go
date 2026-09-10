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
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type CreatePlaylistInput struct {
	Name        string
	Description string
}

type ProviderError struct {
	Status     int
	RetryAfter int
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("Apple Music request failed (HTTP %d)", e.Status)
}

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
	return c.requestMethod(ctx, http.MethodGet, userToken, path, query, nil, destination, http.StatusOK)
}

func (c *Client) requestMethod(ctx context.Context, method, userToken, path string, query url.Values, body []byte, destination any, accepted ...int) error {
	developerToken, err := c.DeveloperToken(time.Now())
	if err != nil {
		return errors.New("could not sign Apple developer token")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path+"?"+query.Encode(), strings.NewReader(string(body)))
	if err != nil {
		return errors.New("could not prepare Apple Music request")
	}
	req.Header.Set("Authorization", "Bearer "+developerToken)
	req.Header.Set("Music-User-Token", userToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("Apple Music request failed")
	}
	defer resp.Body.Close()
	ok := false
	for _, status := range accepted {
		if resp.StatusCode == status {
			ok = true
			break
		}
	}
	if !ok {
		retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if retryAfter < 0 || retryAfter > 86400 {
			retryAfter = 0
		}
		return &ProviderError{Status: resp.StatusCode, RetryAfter: retryAfter}
	}
	if destination == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(destination); err != nil {
		return errors.New("invalid Apple Music response")
	}
	return nil
}

func validResourceID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && !strings.ContainsAny(id, "/?&")
}

// CreatePlaylist creates a private library playlist for the connected user.
func (c *Client) CreatePlaylist(ctx context.Context, userToken, storefront string, input CreatePlaylistInput) (Playlist, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if storefront == "" || len(storefront) > 16 || strings.ContainsAny(storefront, "/?&") || input.Name == "" || len(input.Name) > 255 || len(input.Description) > 1000 {
		return Playlist{}, errors.New("invalid Apple Music playlist input")
	}
	body, err := json.Marshal(struct {
		Attributes struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		} `json:"attributes"`
	}{Attributes: struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{Name: input.Name, Description: input.Description}})
	if err != nil {
		return Playlist{}, errors.New("cannot encode Apple Music playlist request")
	}
	var response struct {
		Data []Playlist `json:"data"`
	}
	if err := c.requestMethod(ctx, http.MethodPost, userToken, "/me/library/playlists", url.Values{"l": {storefront}}, body, &response, http.StatusCreated); err != nil {
		return Playlist{}, err
	}
	if len(response.Data) != 1 || !validResourceID(response.Data[0].ID) {
		return Playlist{}, errors.New("invalid Apple Music playlist response")
	}
	return response.Data[0], nil
}

// AddTracksToPlaylist appends catalog song IDs to a library playlist.
func (c *Client) AddTracksToPlaylist(ctx context.Context, userToken, storefront, playlistID string, trackIDs []string) error {
	if storefront == "" || len(storefront) > 16 || strings.ContainsAny(storefront, "/?&") || !validResourceID(playlistID) || len(trackIDs) == 0 || len(trackIDs) > 100 {
		return errors.New("invalid Apple Music playlist tracks request")
	}
	type trackReference struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	data := make([]trackReference, len(trackIDs))
	for i, id := range trackIDs {
		if !validResourceID(id) {
			return errors.New("invalid Apple Music track ID")
		}
		data[i] = trackReference{ID: id, Type: "songs"}
	}
	body, err := json.Marshal(struct {
		Data []trackReference `json:"data"`
	}{Data: data})
	if err != nil {
		return errors.New("cannot encode Apple Music playlist tracks request")
	}
	return c.requestMethod(ctx, http.MethodPost, userToken, "/me/library/playlists/"+url.PathEscape(playlistID)+"/tracks", url.Values{"l": {storefront}}, body, nil, http.StatusNoContent)
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
