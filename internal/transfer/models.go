package transfer

import "time"

const (
	ProviderSpotify    = "spotify"
	ProviderAppleMusic = "apple-music"
)

type MatchStatus string

const (
	MatchStatusMatched     MatchStatus = "matched"
	MatchStatusAmbiguous   MatchStatus = "ambiguous"
	MatchStatusMissing     MatchStatus = "missing"
	MatchStatusUnsupported MatchStatus = "unsupported"
)

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

type Playlist struct {
	Provider   string `json:"provider"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
}

type PlaylistEntry struct {
	Position    int       `json:"position"`
	Track       *Track    `json:"track,omitempty"`
	AddedAt     *string   `json:"added_at,omitempty"`
	Unavailable bool      `json:"unavailable"`
	Unsupported bool      `json:"unsupported"`
	CapturedAt  time.Time `json:"captured_at,omitempty"`
}

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
