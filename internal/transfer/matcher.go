package transfer

import (
	"sort"
	"strings"
	"unicode"
)

const durationToleranceMS = 2500

type MatchResult struct {
	Status     MatchStatus
	Matched    *Track
	Candidates []Track
	Reason     string
}

func NormalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		return -1
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func MatchTrack(source PlaylistEntry, destinationProvider string, candidates []Track) MatchResult {
	if source.Unsupported || source.Unavailable || source.Track == nil {
		return MatchResult{Status: MatchStatusUnsupported, Reason: "source_entry_unsupported"}
	}
	// Candidate IDs are meaningful only within their provider. Filter before
	// deduplication so an unrelated provider cannot shadow a destination track.
	unique := uniqueTracks(filterTracks(candidates, func(t Track) bool {
		return destinationProvider != "" && t.Provider == destinationProvider
	}))
	if len(unique) == 0 {
		return MatchResult{Status: MatchStatusMissing, Reason: "no_candidates"}
	}
	id := source.Track.ProviderIDs[destinationProvider]
	if strings.TrimSpace(id) == "" && source.Track.Provider == destinationProvider {
		id = source.Track.ID
	}
	if strings.TrimSpace(id) != "" {
		if matched := filterTracks(unique, func(t Track) bool { return t.ID == id || t.ProviderIDs[destinationProvider] == id }); len(matched) > 0 {
			return resolveSingle("provider_id", matched)
		}
	}
	if norm := strings.ToUpper(strings.TrimSpace(source.Track.ISRC)); norm != "" {
		if matched := filterTracks(unique, func(t Track) bool { return strings.ToUpper(strings.TrimSpace(t.ISRC)) == norm }); len(matched) > 0 {
			return resolveSingle("isrc", matched)
		}
	}
	title := NormalizeText(source.Track.Name)
	artists := normalizedArtists(source.Track.Artists)
	meta := filterTracks(unique, func(t Track) bool {
		if NormalizeText(t.Name) != title || title == "" {
			return false
		}
		candidateArtists := normalizedArtists(t.Artists)
		for _, artist := range artists {
			if artist == "" {
				continue
			}
			for _, candidateArtist := range candidateArtists {
				if artist == candidateArtist {
					return true
				}
			}
		}
		return false
	})
	if len(meta) == 0 {
		return MatchResult{Status: MatchStatusMissing, Reason: "no_metadata_match"}
	}
	if source.Track.DurationMS > 0 {
		withinDuration := filterTracks(meta, func(t Track) bool {
			if t.DurationMS <= 0 {
				return false
			}
			delta := t.DurationMS - source.Track.DurationMS
			if delta < 0 {
				delta *= -1
			}
			return delta <= durationToleranceMS
		})
		if len(withinDuration) > 0 {
			return resolveSingle("metadata_duration", withinDuration)
		}
	}
	return MatchResult{Status: MatchStatusAmbiguous, Candidates: meta, Reason: "metadata_without_duration_confidence"}
}

func uniqueTracks(in []Track) []Track {
	seen := map[string]bool{}
	out := make([]Track, 0, len(in))
	for _, track := range in {
		if strings.TrimSpace(track.ID) == "" || seen[track.ID] {
			continue
		}
		seen[track.ID] = true
		if track.Artists == nil {
			track.Artists = []string{}
		}
		out = append(out, track)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func resolveSingle(reason string, options []Track) MatchResult {
	if len(options) == 1 {
		copy := options[0]
		return MatchResult{Status: MatchStatusMatched, Matched: &copy, Reason: reason}
	}
	return MatchResult{Status: MatchStatusAmbiguous, Candidates: options, Reason: reason + "_ambiguous"}
}

func filterTracks(candidates []Track, allow func(Track) bool) []Track {
	matched := make([]Track, 0, len(candidates))
	for _, candidate := range candidates {
		if allow(candidate) {
			matched = append(matched, candidate)
		}
	}
	return matched
}

func normalizedArtists(in []string) []string {
	out := make([]string, 0, len(in))
	for _, artist := range in {
		if normalized := NormalizeText(artist); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}
