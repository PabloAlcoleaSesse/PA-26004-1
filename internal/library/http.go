package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const sessionCookie = "music_session"

func RegisterHTTP(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("GET /api/library/taste", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		limit := 10
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		summary, err := store.Taste(r.Context(), hash, limit)
		if err != nil {
			if errors.Is(err, ErrInvalidTasteLimit) {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			http.Error(w, "could not load taste summary", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})
	mux.HandleFunc("GET /api/library/stats", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		stats, err := store.Stats(r.Context(), hash)
		if err != nil {
			http.Error(w, "could not load library statistics", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, stats)
	})
	mux.HandleFunc("GET /api/library/playlists", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		playlists, err := store.ListPlaylists(r.Context(), hash, limit)
		if err != nil {
			http.Error(w, "could not load library playlists", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, playlists)
	})
	mux.HandleFunc("GET /api/library/playlists/{id}", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		offset, limit := 0, 100
		var err error
		if raw := r.URL.Query().Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
		}
		if err != nil || offset < 0 {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
		}
		if err != nil || limit < 1 || limit > 500 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		playlist, entries, err := store.GetPlaylist(r.Context(), hash, r.PathValue("id"), offset, limit)
		if errors.Is(err, ErrPlaylistNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not load library playlist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Playlist Playlist        `json:"playlist"`
			Entries  []PlaylistEntry `json:"entries"`
		}{playlist, entries})
	})
}

func sessionHashFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Connect a music provider first", http.StatusUnauthorized)
		return "", false
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	return hex.EncodeToString(sum[:]), true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
