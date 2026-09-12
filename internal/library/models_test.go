package library

import "testing"

func TestTrackIDIsStableAndProviderScoped(t *testing.T) {
	spotify := TrackID("spotify", "track-123")
	if spotify != TrackID("spotify", "track-123") {
		t.Fatal("TrackID must be deterministic")
	}
	if spotify == TrackID("apple_music", "track-123") {
		t.Fatal("provider IDs must not collide across providers")
	}
	if len(spotify) != 64 {
		t.Fatalf("expected SHA-256 hex ID, got length %d", len(spotify))
	}
}
