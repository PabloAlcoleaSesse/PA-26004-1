package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var ErrInvalidProvider = errors.New("invalid_provider")

type Service struct {
	store     *Store
	providers map[string]Provider
}

func NewService(store *Store, providers ...Provider) *Service {
	index := make(map[string]Provider, len(providers))
	for _, provider := range providers {
		index[provider.Name()] = provider
	}
	return &Service{store: store, providers: index}
}

func (s *Service) CreatePreview(ctx context.Context, hash, sourceProvider, sourceSnapshotID, destinationProvider string) (TransferPreview, error) {
	if sourceProvider != ProviderSpotify {
		return TransferPreview{}, ErrInvalidProvider
	}
	destination, ok := s.providers[destinationProvider]
	if !ok {
		return TransferPreview{}, ErrInvalidProvider
	}
	sourcePlaylist, sourceEntries, err := s.store.LoadSpotifySnapshot(ctx, hash, sourceSnapshotID)
	if err != nil {
		return TransferPreview{}, err
	}
	results := make([]PreviewEntry, 0, len(sourceEntries))
	cache := map[string]MatchResult{}
	for _, sourceEntry := range sourceEntries {
		result := MatchResult{Status: MatchStatusUnsupported, Reason: "source_entry_unsupported"}
		if sourceEntry.Track != nil && !sourceEntry.Unsupported && !sourceEntry.Unavailable {
			cacheKey := matchingKey(*sourceEntry.Track)
			if cached, ok := cache[cacheKey]; ok {
				result = cached
			} else {
				candidates, err := destination.SearchTracks(ctx, hash, TrackQuery{
					Name:        sourceEntry.Track.Name,
					Artists:     sourceEntry.Track.Artists,
					ISRC:        sourceEntry.Track.ISRC,
					DurationMS:  sourceEntry.Track.DurationMS,
					ProviderIDs: sourceEntry.Track.ProviderIDs,
				}, 10)
				if err != nil {
					if errors.Is(err, ErrUnsupportedOperation) {
						result = MatchResult{Status: MatchStatusUnsupported, Reason: "destination_search_unsupported"}
					} else {
						return TransferPreview{}, err
					}
				} else {
					result = MatchTrack(sourceEntry, destinationProvider, candidates)
				}
				cache[cacheKey] = result
			}
		}
		entry := PreviewEntry{Position: sourceEntry.Position, Source: sourceEntry, Status: result.Status, Reason: result.Reason, Candidates: result.Candidates}
		if result.Matched != nil {
			match := *result.Matched
			entry.Matched = &match
		}
		results = append(results, entry)
	}
	return s.store.SavePreview(ctx, hash, sourceSnapshotID, destinationProvider, sourcePlaylist, results)
}

func matchingKey(track Track) string {
	artists := strings.Join(track.Artists, "|")
	return fmt.Sprintf("%s|%s|%s|%s|%d", track.ID, track.Name, artists, track.ISRC, track.DurationMS)
}

func (s *Service) Preview(ctx context.Context, hash, previewID string, offset, limit int) (TransferPreview, error) {
	return s.store.LoadPreview(ctx, hash, previewID, offset, limit)
}

func (s *Service) EnqueueRun(ctx context.Context, queue *river.Client[pgx.Tx], hash, previewID string) (TransferRunStatus, error) {
	return s.store.EnqueueRun(ctx, queue, hash, previewID)
}

func (s *Service) RunStatus(ctx context.Context, hash, runID string) (TransferRunStatus, error) {
	return s.store.LoadRunStatus(ctx, hash, runID)
}
