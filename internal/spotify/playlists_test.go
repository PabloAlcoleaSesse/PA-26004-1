package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func seedPlaylistSession(t *testing.T, store Store) string {
	t.Helper()
	hash := sessionHash("playlist-browser")
	c := Connection{Profile: Profile{AccountID: "account"}, Tokens: Tokens{AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour)}}
	if err := store.Save(context.Background(), hash, "", c, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return hash
}

func playlistFixture(t *testing.T, mode *string, calls *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("Authorization") != "Bearer private-access" {
			t.Error("missing access token")
		}
		switch r.URL.Path {
		case "/me/playlists":
			fmt.Fprint(w, `{"items":[{"id":"playlist1","name":"My playlist","snapshot_id":"v1","items":{"total":52}}],"total":2,"offset":0,"next":"https://attacker.example/steal"}`)
		case "/playlists/playlist1":
			version, count := "v1", 52
			if *mode == "empty" {
				count = 0
			}
			if *mode == "oversized" {
				count = maxSnapshotItems + 1
			}
			if *mode == "changed" && *calls > 1 {
				version = "v2"
			}
			fmt.Fprintf(w, `{"id":"playlist1","name":"My playlist","snapshot_id":%q,"items":{"total":%d},"external_urls":{"spotify":"https://open.spotify.com/playlist/playlist1"}}`, version, count)
		case "/playlists/playlist1/items":
			if r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("additional_types") != "track,episode" {
				t.Error("incorrect playlist page request")
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if *mode == "rate-limit" {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(429)
				return
			}
			if *mode == "empty-page" {
				fmt.Fprintf(w, `{"items":[],"total":52,"offset":%d}`, offset)
				return
			}
			if *mode == "failed-page" && offset > 0 {
				w.WriteHeader(503)
				return
			}
			count := 50
			if offset == 50 {
				count = 2
			}
			items := make([]map[string]any, count)
			for j := range items {
				item := map[string]any{"type": "track", "id": "duplicate", "uri": "spotify:track:duplicate", "name": "Same recording", "artists": []Artist{{ID: "artist", Name: "Artist"}}, "external_ids": map[string]string{"isrc": "TEST123"}}
				items[j] = map[string]any{"item": item}
			}
			if offset == 0 {
				items[2] = map[string]any{"item": nil}
				items[3] = map[string]any{"is_local": true, "item": map[string]any{"type": "track", "name": "Local recording", "uri": "spotify:local:recording"}}
				items[4] = map[string]any{"item": map[string]any{"type": "episode", "id": "episode", "name": "Podcast"}}
				items[5] = map[string]any{"track": map[string]any{"type": "track", "id": "legacy", "is_playable": false}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": 52, "offset": offset, "next": "https://attacker.example/steal"})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}
}

func TestPlaylistSnapshotPreservesOccurrences(t *testing.T) {
	mode, calls := "normal", 0
	auth, store, _ := testAuth(t, playlistFixture(t, &mode, &calls))
	auth.client.apiURL = strings.TrimSuffix(auth.client.profileURL, "/me")
	hash := seedPlaylistSession(t, store)
	p, entries, err := auth.client.ReadSnapshot(context.Background(), store, hash, "playlist1")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || p.SnapshotID != "v1" || len(entries) != 52 {
		t.Fatalf("calls %d, entries %d", calls, len(entries))
	}
	if entries[0].ID != entries[1].ID || entries[0].Position == entries[1].Position {
		t.Fatal("duplicates not preserved")
	}
	if !entries[2].Unavailable || !entries[3].IsLocal || !entries[4].Unsupported || entries[4].Type != "episode" || !entries[5].Unavailable || entries[5].ID != "legacy" {
		t.Fatal("special entries incorrectly normalized")
	}
	if entries[51].Position != 51 || entries[0].ISRC != "TEST123" {
		t.Fatal("pagination or matching metadata lost")
	}
}

func TestSnapshotFailuresDoNotReturnPartialData(t *testing.T) {
	for _, kind := range []string{"changed", "failed-page", "empty-page", "rate-limit", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			mode, calls := kind, 0
			auth, store, _ := testAuth(t, playlistFixture(t, &mode, &calls))
			auth.client.apiURL = strings.TrimSuffix(auth.client.profileURL, "/me")
			hash := seedPlaylistSession(t, store)
			_, entries, err := auth.client.ReadSnapshot(context.Background(), store, hash, "playlist1")
			if err == nil || entries != nil {
				t.Fatal("partial/invalid snapshot accepted")
			}
			if kind == "changed" && !errors.Is(err, ErrPlaylistChanged) {
				t.Fatal(err)
			}
			if kind == "oversized" && !errors.Is(err, ErrPlaylistTooLarge) {
				t.Fatal(err)
			}
		})
	}
}

func TestEmptyPlaylistAndSafePagination(t *testing.T) {
	mode, calls := "empty", 0
	auth, store, _ := testAuth(t, playlistFixture(t, &mode, &calls))
	auth.client.apiURL = strings.TrimSuffix(auth.client.profileURL, "/me")
	hash := seedPlaylistSession(t, store)
	_, entries, err := auth.client.ReadSnapshot(context.Background(), store, hash, "playlist1")
	if err != nil || entries == nil || len(entries) != 0 || calls != 2 {
		t.Fatal("empty playlist not preserved")
	}
	page, err := auth.client.Playlists(context.Background(), store, hash, 0, 50)
	if err != nil || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatal("incorrect next offset")
	}
	for _, id := range []string{"../me", "playlist?token=x", "https://attacker.example", ""} {
		if _, _, err := auth.client.ReadSnapshot(context.Background(), store, hash, id); err == nil {
			t.Fatal("invalid path accepted")
		}
	}
}

func TestImportHTTPRejectsCSRFAndInvalidPagination(t *testing.T) {
	auth, _, _ := testAuth(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected provider request") })
	mux := http.NewServeMux()
	auth.RegisterImports(mux, nil, nil)
	for _, tc := range []struct {
		method, path, origin string
		cookie               bool
		want                 int
	}{
		{"POST", "/api/spotify/playlists/playlist1/imports", "", true, 403},
		{"POST", "/api/spotify/playlists/playlist1/imports", "https://attacker.example", true, 403},
		{"POST", "/api/spotify/playlists/playlist1/imports", "http://127.0.0.1:8080", false, 401},
		{"GET", "/api/spotify/playlists?offset=-1", "", true, 400},
		{"GET", "/api/spotify/playlists?limit=100", "", true, 400},
		{"GET", "/api/spotify/imports/missing/snapshot?limit=101", "", true, 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "browser"})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}
