package transfer

import (
	"context"
	"strings"
	"testing"
)

type syncProvider struct {
	name        string
	playlist    Playlist
	entries     []PlaylistEntry
	candidates  []Track
	destination []PlaylistEntry
	created     bool
	adds        int
}

func (p *syncProvider) Name() string { return p.name }
func (p *syncProvider) ListPlaylists(context.Context, string, int, int) (PlaylistPage, error) {
	if p.playlist.ID == "" {
		return PlaylistPage{}, nil
	}
	return PlaylistPage{Items: []Playlist{p.playlist}}, nil
}
func (p *syncProvider) ReadPlaylistEntries(_ context.Context, _ string, _ string, offset, limit int) (EntryPage, error) {
	end := offset + limit
	if end > len(p.entries) {
		end = len(p.entries)
	}
	if offset > end {
		offset = end
	}
	return EntryPage{Items: p.entries[offset:end], Total: len(p.entries), Offset: offset}, nil
}
func (p *syncProvider) SearchTracks(context.Context, string, TrackQuery, int) ([]Track, error) {
	return p.candidates, nil
}
func (p *syncProvider) CreatePlaylist(_ context.Context, _ string, input CreatePlaylistInput) (Playlist, error) {
	p.created = true
	p.playlist = Playlist{Provider: p.name, ID: "created", Name: input.Name}
	return p.playlist, nil
}
func (p *syncProvider) AddTracksToPlaylist(_ context.Context, _ string, _ string, ids []string) error {
	p.adds += len(ids)
	for _, id := range ids {
		p.destination = append(p.destination, PlaylistEntry{Position: len(p.destination), Track: &Track{Provider: p.name, ID: id}})
	}
	return nil
}

func TestSyncOncePreflightsConflictsBeforeCreatingDestination(t *testing.T) {
	source := &syncProvider{name: ProviderSpotify, playlist: Playlist{ID: "source", Name: "Road trip"}, entries: []PlaylistEntry{{Position: 0, Track: &Track{Provider: ProviderSpotify, ID: "s1", Name: "Song", Artists: []string{"Artist"}}}}}
	destination := &syncProvider{name: ProviderAppleMusic, candidates: []Track{{Provider: ProviderAppleMusic, ID: "a1", Name: "Different", Artists: []string{"Other"}}}}
	service := &Service{providers: map[string]Provider{ProviderSpotify: source, ProviderAppleMusic: destination}}
	_, _, err := service.SyncOnce(context.Background(), "session", ProviderSpotify, "source", ProviderAppleMusic, "")
	if err == nil || !strings.HasPrefix(err.Error(), "sync_conflict_") {
		t.Fatalf("expected explicit conflict, got %v", err)
	}
	if destination.created || destination.adds != 0 {
		t.Fatal("sync mutated destination before resolving conflict")
	}
}

func TestSyncOnceReconcilesMatchedTracks(t *testing.T) {
	source := &syncProvider{
		name:     ProviderSpotify,
		playlist: Playlist{ID: "source", Name: "Road trip"},
		entries:  []PlaylistEntry{{Position: 0, Track: &Track{Provider: ProviderSpotify, ID: "s1", Name: "Song", Artists: []string{"Artist"}, DurationMS: 180000}}},
	}
	destination := &syncProvider{
		name:       ProviderAppleMusic,
		candidates: []Track{{Provider: ProviderAppleMusic, ID: "a1", Name: "Song", Artists: []string{"Artist"}, DurationMS: 180000}},
	}
	service := &Service{providers: map[string]Provider{ProviderSpotify: source, ProviderAppleMusic: destination}}
	id, matched, err := service.SyncOnce(context.Background(), "session", ProviderSpotify, "source", ProviderAppleMusic, "")
	if err != nil || id != "created" || matched != 1 || destination.adds != 1 {
		t.Fatalf("id=%q matched=%d adds=%d err=%v", id, matched, destination.adds, err)
	}
}
