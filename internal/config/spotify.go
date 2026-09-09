package config

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"os"
)

func LoadTokenEncryptionKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("TOKEN_ENCRYPTION_KEY must be base64 encoding of exactly 32 random bytes")
	}
	return key, nil
}

type Spotify struct {
	ClientID      string
	RedirectURI   string
	EncryptionKey []byte
}

// Spotify is optional: health checks and the queue work without credentials.
// The API and import worker need the same key to read shared credentials.
func LoadSpotify() (Spotify, error) {
	c := Spotify{ClientID: os.Getenv("SPOTIFY_CLIENT_ID"), RedirectURI: os.Getenv("SPOTIFY_REDIRECT_URI")}
	if c.ClientID == "" {
		return c, nil
	}
	u, err := url.Parse(c.RedirectURI)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/auth/spotify/callback" || u.Hostname() == "localhost" {
		return c, errors.New("SPOTIFY_REDIRECT_URI must use /auth/spotify/callback, without credentials, query, or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return c, errors.New("SPOTIFY_REDIRECT_URI requires HTTPS, except for an explicit loopback IP")
	}
	// Require a separate 256-bit key, supplied through the environment. There is
	// no hardcoded fallback: losing this key means users must reconnect.
	c.EncryptionKey, err = LoadTokenEncryptionKey()
	if err != nil {
		return c, err
	}
	return c, nil
}
