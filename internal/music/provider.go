package music

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PlaylistPage uses numeric offsets; a nil NextOffset marks the final page.
// Total may be a lower bound when a provider does not supply an exact total.
type PlaylistPage struct {
	Items      []Playlist
	Total      int
	Offset     int
	NextOffset *int
}

// EntryPage retains provider order, duplicate occurrences, and unavailable entries.
// Position is absolute within the playlist, not relative to this page. Total may
// be a lower bound; a nil NextOffset marks the final page.
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

// Provider resolves credentials internally using an opaque connection reference
// (currently the session hash). Credentials must never be passed in these values.
// Implementations propagate ctx through storage and remote calls. Callers supply
// nonnegative offsets and positive, bounded limits appropriate to the operation.
// Playlist and track IDs are opaque within the named provider. Reads preserve
// source order and duplicate occurrences; appends preserve the input ID order
// and duplicates. Search candidates must have Provider equal to Name() and a
// nonblank ID usable in that provider namespace. Writes may partially succeed
// before returning an error, so callers must reconcile remote state before
// retrying an uncertain write.
// Unsupported operations return an error matching ErrUnsupportedOperation via
// errors.Is; the interface does not guarantee every provider supports each method.
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
