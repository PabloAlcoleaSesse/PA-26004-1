package transfer

import "github.com/PabloAlcoleaSesse/PA-26004-1/internal/music"

// Aliases preserve existing consumers while shared contracts live in music.
type PlaylistPage = music.PlaylistPage
type EntryPage = music.EntryPage
type TrackQuery = music.TrackQuery
type CreatePlaylistInput = music.CreatePlaylistInput
type Provider = music.Provider
type RateLimitError = music.RateLimitError

var ErrUnsupportedOperation = music.ErrUnsupportedOperation
