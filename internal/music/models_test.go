package music_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/music"
	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/transfer"
)

// These fixtures use the existing persisted transfer JSON, including empty-track
// entries and repeated occurrences; extracting the types must not migrate them.
func TestPersistedEntriesRemainCompatible(t *testing.T) {
	const fixture = `[
 {"position":0,"track":{"provider":"spotify","id":"opaque:Track/A","name":"Song","artists":["Artist","Guest"],"album":"Album","duration_ms":123456,"isrc":"USABC1234567","provider_ids":{"spotify":"opaque:Track/A","apple-music":"123"},"url":"https://example.test/track"},"added_at":"2026-01-02T03:04:05Z","unavailable":false,"unsupported":false,"captured_at":"2026-01-03T03:04:05Z"},
 {"position":1,"track":{"provider":"spotify","id":"opaque:Track/A","name":"Song","artists":["Artist","Guest"]},"unavailable":false,"unsupported":false,"captured_at":"2026-01-03T03:04:05Z"},
 {"position":2,"unavailable":true,"unsupported":false,"captured_at":"2026-01-03T03:04:05Z"},
 {"position":3,"track":{"provider":"spotify","id":"local:opaque","name":"Local","artists":[]},"unavailable":false,"unsupported":true,"captured_at":"2026-01-03T03:04:05Z"}
 ]`
	var entries []music.PlaylistEntry
	if err := json.Unmarshal([]byte(fixture), &entries); err != nil {
		t.Fatal(err)
	}
	// This assignment also checks alias identity for existing transfer consumers.
	var existing []transfer.PlaylistEntry = entries
	if len(existing) != 4 || existing[0].Track.ID != existing[1].Track.ID || existing[0].Position != 0 || existing[1].Position != 1 {
		t.Fatalf("lost ordered duplicate occurrences: %+v", existing)
	}
	if existing[2].Track != nil || !existing[2].Unavailable || !existing[3].Unsupported {
		t.Fatalf("lost unavailable/unsupported occurrences: %+v", existing)
	}
	if !reflect.DeepEqual(existing[2].TrackValue(), music.Track{}) {
		t.Fatal("missing metadata must yield zero track")
	}
	if !reflect.DeepEqual(existing[0].TrackValue(), *existing[0].Track) {
		t.Fatal("TrackValue lost metadata")
	}
	encoded, err := json.Marshal(existing)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, []byte(fixture), encoded)
}

func TestPlaylistJSONCompatibility(t *testing.T) {
	const fixture = `{"provider":"spotify","id":"opaque:Playlist/A","name":"Mix","url":"https://example.test/playlist","snapshot_id":"opaque/revision+1"}`
	var playlist music.Playlist
	if err := json.Unmarshal([]byte(fixture), &playlist); err != nil {
		t.Fatal(err)
	}
	var existing transfer.Playlist = playlist
	encoded, err := json.Marshal(existing)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, []byte(fixture), encoded)
}

func TestErrorCompatibility(t *testing.T) {
	if !errors.Is(fmt.Errorf("wrapped: %w", music.ErrUnsupportedOperation), transfer.ErrUnsupportedOperation) {
		t.Fatal("unsupported error identity changed")
	}
	original := &music.RateLimitError{RetryAfter: 45 * time.Second}
	var existing *transfer.RateLimitError
	if !errors.As(fmt.Errorf("wrapped: %w", original), &existing) || existing.RetryAfter != 45*time.Second {
		t.Fatal("rate limit identity or delay changed")
	}
	if existing.Error() != "provider_rate_limited_retry_after_45s" {
		t.Fatalf("error code changed: %s", existing)
	}
}

func assertSameJSON(t *testing.T, want, got []byte) {
	t.Helper()
	var expected, actual any
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("JSON changed:\nwant %s\ngot  %s", want, got)
	}
}
