package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRecentlyPlayedUsesBoundedCursorRequest(t *testing.T) {
	playedAt := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/player/recently-played" || r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("after") == "" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		fmt.Fprintf(w, `{"items":[{"played_at":%q,"track":{"type":"track","id":"t1","name":"Song","artists":[{"name":"Artist"}],"album":{"name":"Album"},"duration_ms":180000,"external_ids":{"isrc":"US123"},"external_urls":{"spotify":"https://open.spotify.com/track/t1"}}}],"cursors":{"after":"123","before":"100"}}`, playedAt.Format(time.RFC3339))
	}))
	defer server.Close()
	store := newMemoryStore()
	hash := "session"
	if err := store.Save(context.Background(), hash, "", Connection{Tokens: Tokens{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	client := NewClient("client", "http://localhost/callback")
	client.apiURL = server.URL
	page, err := client.RecentlyPlayed(context.Background(), store, hash, &playedAt, 2)
	if err != nil || len(page.Items) != 1 || page.Items[0].Track.ID != "t1" || !page.Items[0].PlayedAt.Equal(playedAt) {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
