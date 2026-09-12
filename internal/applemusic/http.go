package applemusic

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Register exposes a small server-side bridge for MusicKit user tokens. Apple
// user tokens are accepted only over POST and are encrypted immediately.
func (c *Client) Register(mux *http.ServeMux, store Store, origin string) {
	mux.HandleFunc("POST /api/connections/apple-music", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		var input struct {
			UserToken  string `json:"user_token"`
			Storefront string `json:"storefront"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil || input.UserToken == "" || input.Storefront == "" {
			http.Error(w, "user_token and storefront are required", http.StatusBadRequest)
			return
		}
		if len(input.UserToken) > 4096 || len(input.Storefront) > 16 {
			http.Error(w, "invalid Apple Music connection", http.StatusBadRequest)
			return
		}
		// Validate the token before storing it, so a typo cannot create a dead session.
		if _, err := c.Playlists(r.Context(), input.UserToken, input.Storefront, 0, 1); err != nil {
			http.Error(w, "Apple Music authorization failed", http.StatusBadGateway)
			return
		}
		session := randomSession()
		hash := hashSession(session)
		oldHash := ""
		if cookie, err := r.Cookie("music_session"); err == nil {
			oldHash = hashSession(cookie.Value)
		}
		if err := store.Save(r.Context(), hash, oldHash, Connection{UserToken: input.UserToken, Storefront: input.Storefront}, time.Now().Add(30*24*time.Hour)); err != nil {
			http.Error(w, "Could not save Apple Music connection", 500)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "music_session", Value: session, Path: "/", MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
		writeJSON(w, 201, map[string]any{"connected": true, "storefront": input.Storefront})
	})
	mux.HandleFunc("GET /api/apple-music/playlists", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("music_session")
		if err != nil {
			http.Error(w, "Connect Apple Music first", 401)
			return
		}
		connection, err := store.Load(r.Context(), hashSession(cookie.Value))
		if errors.Is(err, ErrNoConnection) {
			http.Error(w, "Connect Apple Music first", 401)
			return
		}
		if err != nil {
			http.Error(w, "Could not load Apple Music connection", 500)
			return
		}
		page, err := c.Playlists(r.Context(), connection.UserToken, connection.Storefront, 0, 50)
		if err != nil {
			http.Error(w, "Could not list Apple Music playlists", 502)
			return
		}
		writeJSON(w, 200, page)
	})
	mux.HandleFunc("DELETE /api/connections/apple-music", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", 403)
			return
		}
		if cookie, err := r.Cookie("music_session"); err == nil {
			_ = store.Delete(r.Context(), hashSession(cookie.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: "music_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
		w.WriteHeader(204)
	})
}

// RegisterImports exposes queued playlist catalog imports. The queue payload
// contains only an opaque import ID; MusicKit credentials stay encrypted.
func RegisterImports(mux *http.ServeMux, importer *Importer, queue *river.Client[pgx.Tx]) {
	mux.HandleFunc("POST /api/apple-music/imports", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("music_session")
		if err != nil {
			http.Error(w, "Connect Apple Music first", http.StatusUnauthorized)
			return
		}
		var input struct {
			PlaylistID string `json:"playlist_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil || input.PlaylistID == "" {
			http.Error(w, "playlist_id is required", http.StatusBadRequest)
			return
		}
		id, err := importer.Enqueue(r.Context(), queue, hashSession(cookie.Value), input.PlaylistID)
		if errors.Is(err, ErrNoConnection) {
			http.Error(w, "Connect Apple Music first", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "Could not queue Apple Music import", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "state": "queued"})
	})
	mux.HandleFunc("GET /api/apple-music/imports/{id}", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("music_session")
		if err != nil {
			http.Error(w, "Connect Apple Music first", http.StatusUnauthorized)
			return
		}
		status, err := importer.Status(r.Context(), hashSession(cookie.Value), r.PathValue("id"))
		if errors.Is(err, ErrImportNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Could not load Apple Music import", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func randomSession() string {
	b := make([]byte, 32)
	_, _ = cryptoRandRead(b)
	return hex.EncodeToString(b)
}

var cryptoRandRead = func(b []byte) (int, error) { return rand.Read(b) }

func hashSession(s string) string { d := sha256.Sum256([]byte(s)); return hex.EncodeToString(d[:]) }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
