package transfer

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type PlaylistPage struct {
	Items      []Playlist
	Total      int
	Offset     int
	NextOffset *int
}

type EntryPage struct {
	Items      []PlaylistEntry
	Total      int
	Offset     int
	NextOffset *int
}

type TrackQuery struct {
	Name        string
	Artists     []string
	ISRC        string
	DurationMS  int
	ProviderIDs map[string]string
}

type CreatePlaylistInput struct {
	Name        string
	Description string
}

type Provider interface {
	Name() string
	ListPlaylists(context.Context, string, int, int) (PlaylistPage, error)
	ReadPlaylistEntries(context.Context, string, string, int, int) (EntryPage, error)
	SearchTracks(context.Context, string, TrackQuery, int) ([]Track, error)
	CreatePlaylist(context.Context, string, CreatePlaylistInput) (Playlist, error)
	AddTracksToPlaylist(context.Context, string, string, []string) error
}

var ErrUnsupportedOperation = errors.New("operation_unsupported")

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter <= 0 {
		return "provider_rate_limited"
	}
	return fmt.Sprintf("provider_rate_limited_retry_after_%ds", int(e.RetryAfter.Seconds()))
}
