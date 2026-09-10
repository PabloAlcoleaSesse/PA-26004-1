package applemusic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestDeveloperTokenAndValidation(t *testing.T) {
	client, err := NewClient("team", "key", testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.DeveloperToken(time.Unix(1000, 0))
	if err != nil || strings.Count(token, ".") != 2 {
		t.Fatal("invalid developer JWT")
	}
	for _, tc := range []struct {
		storefront string
	}{{""}, {"es?x"}} {
		_, err := client.Playlists(context.Background(), "user", tc.storefront, 0, 50)
		if err == nil {
			t.Fatalf("invalid storefront accepted: %q", tc.storefront)
		}
	}
}

func TestAppleRequestHeadersAndPagination(t *testing.T) {
	var gotAuth, gotUser string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUser = r.Header.Get("Music-User-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"p1","attributes":{"name":"Library","url":"https://music.apple.com/playlist/p1"}}],"next":"/v1/me/library/playlists?offset=1"}`))
	}))
	defer server.Close()
	client, _ := NewClient("team", "key", testKey(t))
	client.baseURL = server.URL
	page, err := client.Playlists(context.Background(), "user-token", "es", 0, 50)
	if err != nil || len(page.Data) != 1 || page.Next == "" {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") || gotUser != "user-token" {
		t.Fatal("missing Apple authorization headers")
	}
}

func TestApplePlaylistWrites(t *testing.T) {
	var createdName, createdDescription string
	var added []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Music-User-Token") != "user-token" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing Apple authorization headers")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /me/library/playlists":
			var body struct {
				Attributes struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"attributes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			createdName, createdDescription = body.Attributes.Name, body.Attributes.Description
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":[{"id":"p.created","attributes":{"name":"Created"}}]}`))
		case "POST /me/library/playlists/p.created/tracks":
			var body struct {
				Data []map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			added = body.Data
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient("team", "key", testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	playlist, err := client.CreatePlaylist(context.Background(), "user-token", "es", CreatePlaylistInput{Name: "  Transfer  ", Description: "Copied playlist"})
	if err != nil {
		t.Fatal(err)
	}
	if playlist.ID != "p.created" || createdName != "Transfer" || createdDescription != "Copied playlist" {
		t.Fatalf("unexpected playlist response: %+v", playlist)
	}
	if err := client.AddTracksToPlaylist(context.Background(), "user-token", "es", playlist.ID, []string{"song1", "song2"}); err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 || added[0]["id"] != "song1" || added[0]["type"] != "songs" || added[1]["id"] != "song2" {
		t.Fatalf("unexpected added tracks: %#v", added)
	}
}
