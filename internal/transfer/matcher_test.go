package transfer

import "testing"

func TestNormalizeText(t *testing.T) {
	if got := NormalizeText("  Héllo,  WORLD!!! "); got != "héllo world" {
		t.Fatalf("got %q", got)
	}
}

func TestMatchTrackPriorityAndConfidence(t *testing.T) {
	source := PlaylistEntry{Track: &Track{Provider: ProviderSpotify, ID: "sp1", Name: "Song Name", Artists: []string{"Artist Name"}, ISRC: "isrc1", DurationMS: 200000, ProviderIDs: map[string]string{ProviderSpotify: "sp1"}}}
	candidates := []Track{
		{Provider: ProviderSpotify, ID: "other", Name: "song name", Artists: []string{"artist name"}, ISRC: "isrc1", DurationMS: 200000, ProviderIDs: map[string]string{ProviderSpotify: "other"}},
		{Provider: ProviderSpotify, ID: "sp1", Name: "different", Artists: []string{"other"}, DurationMS: 1000, ProviderIDs: map[string]string{ProviderSpotify: "sp1"}},
	}
	result := MatchTrack(source, ProviderSpotify, candidates)
	if result.Status != MatchStatusMatched || result.Matched == nil || result.Matched.ID != "sp1" || result.Reason != "provider_id" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestMatchTrackISRCMetadataDurationAmbiguousAndMissing(t *testing.T) {
	source := PlaylistEntry{Track: &Track{Name: "Track", Artists: []string{"Artist"}, ISRC: "AA11", DurationMS: 180000, ProviderIDs: map[string]string{}}}
	isrc := MatchTrack(source, ProviderAppleMusic, []Track{{ID: "a", ISRC: "aa11", Name: "other", Artists: []string{"x"}}})
	if isrc.Status != MatchStatusMatched || isrc.Matched.ID != "a" {
		t.Fatalf("isrc mismatch: %+v", isrc)
	}
	metadata := MatchTrack(source, ProviderAppleMusic, []Track{{ID: "a", Name: "Track", Artists: []string{"Artist"}, DurationMS: 180500}})
	if metadata.Status != MatchStatusMatched || metadata.Matched.ID != "a" || metadata.Reason != "metadata_duration" {
		t.Fatalf("metadata mismatch: %+v", metadata)
	}
	ambiguous := MatchTrack(source, ProviderAppleMusic, []Track{{ID: "a", Name: "Track", Artists: []string{"Artist"}, DurationMS: 181000}, {ID: "b", Name: "Track", Artists: []string{"Artist"}, DurationMS: 179500}})
	if ambiguous.Status != MatchStatusAmbiguous || len(ambiguous.Candidates) != 2 {
		t.Fatalf("expected ambiguity: %+v", ambiguous)
	}
	missing := MatchTrack(source, ProviderAppleMusic, []Track{{ID: "a", Name: "Different", Artists: []string{"Artist"}}})
	if missing.Status != MatchStatusMissing {
		t.Fatalf("expected missing: %+v", missing)
	}
}

func TestMatchTrackUnsupportedAndDedupedCandidates(t *testing.T) {
	unsupported := MatchTrack(PlaylistEntry{Unsupported: true, Track: &Track{Name: "x"}}, ProviderAppleMusic, []Track{{ID: "a"}})
	if unsupported.Status != MatchStatusUnsupported {
		t.Fatalf("expected unsupported: %+v", unsupported)
	}
	source := PlaylistEntry{Track: &Track{Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000, ProviderIDs: map[string]string{}}}
	result := MatchTrack(source, ProviderAppleMusic, []Track{{ID: "dup", Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000}, {ID: "dup", Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000}})
	if result.Status != MatchStatusMatched || result.Matched.ID != "dup" {
		t.Fatalf("expected deduped match: %+v", result)
	}
}
