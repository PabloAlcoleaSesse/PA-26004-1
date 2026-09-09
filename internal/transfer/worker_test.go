package transfer

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	name      string
	playlists []Playlist
	entries   map[string][]PlaylistEntry
	addErr    error
	addCalls  int
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) ListPlaylists(context.Context, string, int, int) (PlaylistPage, error) {
	items := append([]Playlist(nil), p.playlists...)
	return PlaylistPage{Items: items}, nil
}
func (p *fakeProvider) ReadPlaylistEntries(_ context.Context, _ string, playlistID string, offset, limit int) (EntryPage, error) {
	all := p.entries[playlistID]
	if offset > len(all) {
		offset = len(all)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	items := append([]PlaylistEntry(nil), all[offset:end]...)
	return EntryPage{Items: items, Total: len(all), Offset: offset}, nil
}
func (p *fakeProvider) SearchTracks(context.Context, string, TrackQuery, int) ([]Track, error) {
	return nil, ErrUnsupportedOperation
}
func (p *fakeProvider) CreatePlaylist(context.Context, string, CreatePlaylistInput) (Playlist, error) {
	return Playlist{}, ErrUnsupportedOperation
}
func (p *fakeProvider) AddTracksToPlaylist(_ context.Context, _ string, playlistID string, trackIDs []string) error {
	p.addCalls++
	if p.addErr != nil {
		return p.addErr
	}
	for _, id := range trackIDs {
		track := Track{Provider: p.name, ID: id, ProviderIDs: map[string]string{p.name: id}}
		p.entries[playlistID] = append(p.entries[playlistID], PlaylistEntry{Position: len(p.entries[playlistID]), Track: &track})
	}
	return nil
}

func TestEnsureEntryStateIdempotentAndOrdered(t *testing.T) {
	provider := &fakeProvider{name: ProviderAppleMusic, entries: map[string][]PlaylistEntry{"dest": {}}}
	if err := ensureEntryState(context.Background(), provider, "session", "dest", 0, "a"); err != nil {
		t.Fatal(err)
	}
	if err := ensureEntryState(context.Background(), provider, "session", "dest", 0, "a"); err != nil {
		t.Fatal(err)
	}
	if provider.addCalls != 1 {
		t.Fatalf("expected one write, got %d", provider.addCalls)
	}
	if len(provider.entries["dest"]) != 1 || provider.entries["dest"][0].Track.ID != "a" {
		t.Fatal("entry not preserved")
	}
}

func TestEnsureEntryStateConflictAndFailure(t *testing.T) {
	provider := &fakeProvider{name: ProviderAppleMusic, entries: map[string][]PlaylistEntry{"dest": {{Track: &Track{ID: "existing"}}}}}
	if err := ensureEntryState(context.Background(), provider, "session", "dest", 0, "different"); err == nil {
		t.Fatal("expected conflict")
	}
	provider = &fakeProvider{name: ProviderAppleMusic, entries: map[string][]PlaylistEntry{"dest": {}}, addErr: &RateLimitError{}}
	err := ensureEntryState(context.Background(), provider, "session", "dest", 0, "track")
	var rateLimit *RateLimitError
	if err == nil || !errors.As(err, &rateLimit) {
		t.Fatalf("expected propagated write error, got %v", err)
	}
}

func TestFindPlaylistByName(t *testing.T) {
	provider := &fakeProvider{name: ProviderAppleMusic, playlists: []Playlist{{ID: "1", Name: "One"}, {ID: "2", Name: "Two"}}}
	id, err := findPlaylistByName(context.Background(), provider, "session", "Two")
	if err != nil || id != "2" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	id, err = findPlaylistByName(context.Background(), provider, "session", "Missing")
	if err != nil || id != "" {
		t.Fatalf("expected empty, got %q err=%v", id, err)
	}
}
