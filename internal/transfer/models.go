package transfer

import (
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/music"
)

const (
	ProviderSpotify    = music.ProviderSpotify
	ProviderAppleMusic = music.ProviderAppleMusic
)

type MatchStatus string

const (
	MatchStatusMatched     MatchStatus = "matched"
	MatchStatusAmbiguous   MatchStatus = "ambiguous"
	MatchStatusMissing     MatchStatus = "missing"
	MatchStatusUnsupported MatchStatus = "unsupported"
)

// Shared aliases preserve the existing transfer API and persisted JSON shapes.
type Track = music.Track
type Playlist = music.Playlist
type PlaylistEntry = music.PlaylistEntry

type TransferPreview struct {
	ID                  string         `json:"id"`
	Source              Playlist       `json:"source"`
	DestinationProvider string         `json:"destination_provider"`
	State               string         `json:"state"`
	CreatedAt           time.Time      `json:"created_at"`
	Entries             []PreviewEntry `json:"entries"`
}

type PreviewEntry struct {
	Position   int           `json:"position"`
	Source     PlaylistEntry `json:"source"`
	Status     MatchStatus   `json:"status"`
	Matched    *Track        `json:"matched,omitempty"`
	Candidates []Track       `json:"candidates,omitempty"`
	Reason     string        `json:"reason,omitempty"`
}

// MatchDecision records the destination candidate selected by the user for a
// preview entry that could not be resolved with sufficient confidence.
type MatchDecision struct {
	PreviewID           string    `json:"preview_id"`
	Position            int       `json:"position"`
	DestinationProvider string    `json:"destination_provider"`
	Selected            Track     `json:"selected"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type TransferRunStatus struct {
	ID                    string    `json:"id"`
	PreviewID             string    `json:"preview_id"`
	State                 string    `json:"state"`
	ErrorCode             string    `json:"error_code,omitempty"`
	DestinationPlaylistID string    `json:"destination_playlist_id,omitempty"`
	NextPosition          int       `json:"next_position"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}
