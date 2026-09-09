package config

import (
	"encoding/base64"
	"testing"
)

func TestSpotifyConfiguration(t *testing.T) {
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	t.Setenv("SPOTIFY_REDIRECT_URI", "")
	t.Setenv("TOKEN_ENCRYPTION_KEY", "")
	if c, err := LoadSpotify(); err != nil || c.ClientID != "" {
		t.Fatal("unconfigured Spotify must be optional")
	}
	t.Setenv("SPOTIFY_CLIENT_ID", "client")
	t.Setenv("TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, tc := range []struct {
		uri   string
		valid bool
	}{
		{"http://127.0.0.1:8080/auth/spotify/callback", true},
		{"http://[::1]:8080/auth/spotify/callback", true},
		{"https://music.example/auth/spotify/callback", true},
		{"http://localhost:8080/auth/spotify/callback", false},
		{"http://0.0.0.0:8080/auth/spotify/callback", false},
		{"http://music.example/auth/spotify/callback", false},
		{"https://music.example/wrong-path", false},
		{"https://music.example/auth/spotify/callback?next=evil", false},
		{"https://user:pass@music.example/auth/spotify/callback", false},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			t.Setenv("SPOTIFY_REDIRECT_URI", tc.uri)
			_, err := LoadSpotify()
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected validity: %v", err)
			}
		})
	}
	t.Setenv("SPOTIFY_REDIRECT_URI", "https://music.example/auth/spotify/callback")
	t.Setenv("TOKEN_ENCRYPTION_KEY", "short")
	if _, err := LoadSpotify(); err == nil {
		t.Fatal("invalid encryption key accepted")
	}
}
