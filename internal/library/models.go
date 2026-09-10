package library

import "time"

type Track struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Artists    []string  `json:"artists"`
	Album      string    `json:"album,omitempty"`
	DurationMS int       `json:"duration_ms,omitempty"`
	ISRC       string    `json:"isrc,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Playlist struct {
	ID             string    `json:"id"`
	SourceProvider string    `json:"source_provider"`
	SourceID       string    `json:"source_id"`
	Name           string    `json:"name"`
	URL            string    `json:"url,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PlaylistEntry struct {
	Position        int    `json:"position"`
	Track           *Track `json:"track,omitempty"`
	Provider        string `json:"provider"`
	ProviderTrackID string `json:"provider_track_id,omitempty"`
	Unavailable     bool   `json:"unavailable"`
	Unsupported     bool   `json:"unsupported"`
}
