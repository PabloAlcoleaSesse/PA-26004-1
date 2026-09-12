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
	isrc := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "a", ISRC: "aa11", Name: "other", Artists: []string{"x"}}})
	if isrc.Status != MatchStatusMatched || isrc.Matched.ID != "a" {
		t.Fatalf("isrc mismatch: %+v", isrc)
	}
	metadata := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "a", Name: "Track", Artists: []string{"Artist"}, DurationMS: 180500}})
	if metadata.Status != MatchStatusMatched || metadata.Matched.ID != "a" || metadata.Reason != "metadata_duration" {
		t.Fatalf("metadata mismatch: %+v", metadata)
	}
	ambiguous := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "a", Name: "Track", Artists: []string{"Artist"}, DurationMS: 181000}, {Provider: ProviderAppleMusic, ID: "b", Name: "Track", Artists: []string{"Artist"}, DurationMS: 179500}})
	if ambiguous.Status != MatchStatusAmbiguous || len(ambiguous.Candidates) != 2 {
		t.Fatalf("expected ambiguity: %+v", ambiguous)
	}
	missing := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "a", Name: "Different", Artists: []string{"Artist"}}})
	if missing.Status != MatchStatusMissing {
		t.Fatalf("expected missing: %+v", missing)
	}
}

func TestMatchTrackUnsupportedAndDedupedCandidates(t *testing.T) {
	unsupported := MatchTrack(PlaylistEntry{Unsupported: true, Track: &Track{Name: "x"}}, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "a"}})
	if unsupported.Status != MatchStatusUnsupported {
		t.Fatalf("expected unsupported: %+v", unsupported)
	}
	source := PlaylistEntry{Track: &Track{Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000, ProviderIDs: map[string]string{}}}
	result := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "dup", Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000}, {Provider: ProviderAppleMusic, ID: "dup", Name: "Same", Artists: []string{"Artist"}, DurationMS: 1000}})
	if result.Status != MatchStatusMatched || result.Matched.ID != "dup" {
		t.Fatalf("expected deduped match: %+v", result)
	}
}

func TestMatchTrackRequiresDestinationCandidateIdentity(t *testing.T) {
	source := PlaylistEntry{Track: &Track{Provider: ProviderSpotify, ID: "source", Name: "Song", Artists: []string{"Artist"}, DurationMS: 1000, ISRC: "ISRC1", ProviderIDs: map[string]string{ProviderAppleMusic: "destination"}}}
	for _, provider := range []string{ProviderSpotify, ""} {
		candidate := Track{Provider: provider, ID: "destination", Name: "Song", Artists: []string{"Artist"}, DurationMS: 1000, ISRC: "ISRC1"}
		if result := MatchTrack(source, ProviderAppleMusic, []Track{candidate}); result.Status != MatchStatusMissing {
			t.Fatalf("provider %q must not match destination: %+v", provider, result)
		}
	}
	// The wrong provider must not consume an ID during deduplication.
	result := MatchTrack(source, ProviderAppleMusic, []Track{
		{Provider: ProviderSpotify, ID: "destination"},
		{Provider: ProviderAppleMusic, ID: "destination"},
	})
	if result.Status != MatchStatusMatched || result.Matched.Provider != ProviderAppleMusic || result.Reason != "provider_id" {
		t.Fatalf("expected destination-scoped match: %+v", result)
	}
}

func TestMatchTrackIgnoresBlankIdentifiers(t *testing.T) {
	source := PlaylistEntry{Track: &Track{ISRC: " \t\n"}}
	result := MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "destination", ISRC: ""}})
	if result.Status != MatchStatusMissing {
		t.Fatalf("blank ISRC must not establish confidence: %+v", result)
	}
	source.Track.ISRC = " ISRC1 "
	result = MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: " \t", ISRC: "ISRC1"}})
	if result.Status != MatchStatusMissing {
		t.Fatalf("blank destination ID must not match: %+v", result)
	}
	result = MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "destination", ISRC: " isrc1 "}})
	if result.Status != MatchStatusMatched || result.Reason != "isrc" {
		t.Fatalf("nonblank normalized ISRC should match: %+v", result)
	}
}

func TestMatchTrackUsesPrimarySourceIdentity(t *testing.T) {
	source := PlaylistEntry{Track: &Track{Provider: ProviderSpotify, ID: "source"}}
	result := MatchTrack(source, ProviderSpotify, []Track{{Provider: ProviderSpotify, ID: "source"}})
	if result.Status != MatchStatusMatched || result.Reason != "provider_id" {
		t.Fatalf("primary source identity should match: %+v", result)
	}
	source.Track.ProviderIDs = map[string]string{ProviderSpotify: "preferred"}
	result = MatchTrack(source, ProviderSpotify, []Track{{Provider: ProviderSpotify, ID: "source"}, {Provider: ProviderSpotify, ID: "preferred"}})
	if result.Status != MatchStatusMatched || result.Matched.ID != "preferred" {
		t.Fatalf("explicit destination identity retains priority: %+v", result)
	}
}

func TestMatchTrackPreservesOpaqueIDsAndCandidateAliases(t *testing.T) {
	source := PlaylistEntry{Track: &Track{Provider: ProviderSpotify, ID: " source "}}
	result := MatchTrack(source, ProviderSpotify, []Track{{Provider: ProviderSpotify, ID: "source"}})
	if result.Status != MatchStatusMissing {
		t.Fatalf("opaque IDs must compare without trimming: %+v", result)
	}
	source.Track.ProviderIDs = map[string]string{ProviderAppleMusic: "alias"}
	result = MatchTrack(source, ProviderAppleMusic, []Track{{Provider: ProviderAppleMusic, ID: "primary", ProviderIDs: map[string]string{ProviderAppleMusic: "alias"}}})
	if result.Status != MatchStatusMatched || result.Matched.ID != "primary" || result.Reason != "provider_id" {
		t.Fatalf("destination-scoped alias should retain existing matching behavior: %+v", result)
	}
}
