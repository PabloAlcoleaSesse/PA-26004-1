package config

import (
	"errors"
	"os"
)

type AppleMusic struct {
	TeamID        string
	KeyID         string
	PrivateKeyPEM string
}

// Apple Music has no server-side OAuth redirect. A MusicKit client obtains a
// user token; the API uses these credentials only to sign catalog requests.
func LoadAppleMusic() (AppleMusic, error) {
	c := AppleMusic{TeamID: os.Getenv("APPLE_TEAM_ID"), KeyID: os.Getenv("APPLE_KEY_ID"), PrivateKeyPEM: os.Getenv("APPLE_PRIVATE_KEY")}
	if c.TeamID == "" && c.KeyID == "" && c.PrivateKeyPEM == "" {
		return c, nil
	}
	if c.TeamID == "" || c.KeyID == "" || c.PrivateKeyPEM == "" {
		return c, errors.New("APPLE_TEAM_ID, APPLE_KEY_ID, and APPLE_PRIVATE_KEY must be set together")
	}
	return c, nil
}
