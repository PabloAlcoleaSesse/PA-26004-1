// Package music defines provider-neutral music values and provider operations.
// It does not depend on provider clients or transfer orchestration.
package music

import "time"

const (
	ProviderSpotify    = "spotify"
	ProviderAppleMusic = "apple-music"
)

// Track describes a provider recording. ID and ProviderIDs are opaque provider
// identifiers, not catalog database IDs; preserve their spelling and namespace.
type Track struct {
	Provider    string            `json:"provider"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Artists     []string          `json:"artists"`
	Album       string            `json:"album,omitempty"`
	DurationMS  int               `json:"duration_ms,omitempty"`
	ISRC        string            `json:"isrc,omitempty"`
	ProviderIDs map[string]string `json:"provider_ids,omitempty"`
	URL         string            `json:"url,omitempty"`
}

// Playlist identifies a provider playlist; SnapshotID is an optional opaque revision.
type Playlist struct {
	Provider   string `json:"provider"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
}

// PlaylistEntry is an occurrence at a zero-based source position. Repeated tracks
// remain separate entries. Keep unavailable and unsupported occurrences even when
// no track metadata is available.
type PlaylistEntry struct {
	Position    int       `json:"position"`
	Track       *Track    `json:"track,omitempty"`
	AddedAt     *string   `json:"added_at,omitempty"`
	Unavailable bool      `json:"unavailable"`
	Unsupported bool      `json:"unsupported"`
	CapturedAt  time.Time `json:"captured_at,omitempty"`
}

// TrackValue returns the entry's track or its zero value when metadata is absent.
// Callers must inspect the entry flags when deciding whether it can be transferred.
func (entry PlaylistEntry) TrackValue() Track {
	if entry.Track == nil {
		return Track{}
	}
	return *entry.Track
}
