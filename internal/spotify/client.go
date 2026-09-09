// Package spotify implements Spotify authorization and account connections.
// Provider credentials stay on the server; browsers receive an opaque session.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Profile excludes personal fields we do not need. Spotify's immutable
// account_id identifies the account; its older id field can change.
type Profile struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
}

// Tokens is serialized only inside authenticated encryption, never to a browser.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}

// ProviderError carries safe metadata, not response bodies or request URLs.
type ProviderError struct {
	Status     int
	RetryAfter int
	Reconnect  bool
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("Spotify request failed (HTTP %d)", e.Status)
}

type Client struct {
	clientID     string
	redirectURI  string
	authorizeURL string
	tokenURL     string
	profileURL   string
	http         *http.Client
}

func NewClient(clientID, redirectURI string) *Client {
	return &Client{
		clientID: clientID, redirectURI: redirectURI,
		authorizeURL: "https://accounts.spotify.com/authorize",
		tokenURL:     "https://accounts.spotify.com/api/token",
		profileURL:   "https://api.spotify.com/v1/me",
		http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			// Never forward bearer tokens or authorization codes through redirects.
			return http.ErrUseLastResponse
		}},
	}
}

func (c *Client) AuthorizationURL(state, challenge string) string {
	q := url.Values{
		"client_id": {c.clientID}, "redirect_uri": {c.redirectURI},
		"response_type": {"code"}, "state": {state},
		"code_challenge_method": {"S256"}, "code_challenge": {challenge},
		// Read-only permissions cover connection and the upcoming import step.
		"scope": {"playlist-read-private playlist-read-collaborative"},
	}
	return c.authorizeURL + "?" + q.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (Tokens, error) {
	t, err := c.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {c.redirectURI}, "code_verifier": {verifier},
	})
	if err == nil && t.RefreshToken == "" {
		return Tokens{}, errors.New("Spotify did not return a refresh token")
	}
	return t, err
}

func (c *Client) Refresh(ctx context.Context, previous Tokens) (Tokens, error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {previous.RefreshToken}})
	if err != nil {
		return Tokens{}, err
	}
	// Spotify may omit a replacement refresh token or scope on refresh.
	if t.RefreshToken == "" {
		t.RefreshToken = previous.RefreshToken
	}
	if t.Scope == "" {
		t.Scope = previous.Scope
	}
	return t, nil
}

func (c *Client) token(ctx context.Context, form url.Values) (Tokens, error) {
	form.Set("client_id", c.clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, errors.New("cannot prepare Spotify token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := c.request(req, &body); err != nil {
		return Tokens{}, err
	}
	if body.AccessToken == "" || !strings.EqualFold(body.TokenType, "Bearer") || body.ExpiresIn <= 0 || body.ExpiresIn > 86400 {
		return Tokens{}, errors.New("invalid Spotify token response")
	}
	return Tokens{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Scope: body.Scope, ExpiresAt: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)}, nil
}

func (c *Client) Profile(ctx context.Context, accessToken string) (Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.profileURL, nil)
	if err != nil {
		return Profile{}, errors.New("cannot prepare Spotify profile request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	var profile Profile
	if err := c.request(req, &profile); err != nil {
		return Profile{}, err
	}
	if profile.AccountID == "" {
		return Profile{}, errors.New("Spotify profile has no stable account ID")
	}
	return profile, nil
}

func (c *Client) request(req *http.Request, destination any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("Spotify request could not be completed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		// Inspect only the OAuth error code; never expose error descriptions.
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
		retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if retry < 0 || retry > 86400 {
			retry = 0
		}
		return &ProviderError{Status: resp.StatusCode, RetryAfter: retry, Reconnect: body.Error == "invalid_grant"}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(destination); err != nil {
		return errors.New("invalid Spotify response")
	}
	return nil
}
