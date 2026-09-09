package spotify

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// RegisterImports adds authenticated read/import endpoints. Only the API
// enqueues jobs; the separate worker performs the potentially long import.
func (a *Auth) RegisterImports(mux *http.ServeMux, importer *Importer, queue *river.Client[pgx.Tx]) {
	mux.HandleFunc("GET /api/spotify/playlists", a.wrap(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := a.session(w, r)
		if !ok {
			return
		}
		offset, limit, ok := pagination(w, r, 50)
		if !ok {
			return
		}
		page, err := a.client.Playlists(r.Context(), a.store, hash, offset, limit)
		if err != nil {
			a.failure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}))
	mux.HandleFunc("POST /api/spotify/playlists/{id}/imports", a.wrap(func(w http.ResponseWriter, r *http.Request) {
		// Import changes local state and consumes provider quota: enforce CSRF.
		if r.Header.Get("Origin") != a.origin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		hash, ok := a.session(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !validPlaylistID(id) {
			http.Error(w, "Invalid playlist ID", http.StatusBadRequest)
			return
		}
		importID, err := importer.Enqueue(r.Context(), queue, hash, id)
		if errors.Is(err, ErrTooManyImports) {
			http.Error(w, "Too many active imports", http.StatusTooManyRequests)
			return
		}
		if err != nil {
			a.failure(w, err)
			return
		}
		location := "/api/spotify/imports/" + importID
		w.Header().Set("Location", location)
		writeJSON(w, http.StatusAccepted, map[string]string{"import_id": importID, "status_url": location, "snapshot_url": location + "/snapshot"})
	}))
	mux.HandleFunc("GET /api/spotify/imports/{id}", a.wrap(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := a.session(w, r)
		if !ok {
			return
		}
		status, err := importer.Status(r.Context(), hash, r.PathValue("id"))
		if errors.Is(err, ErrImportNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Could not read import", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("GET /api/spotify/imports/{id}/snapshot", a.wrap(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := a.session(w, r)
		if !ok {
			return
		}
		offset, limit, ok := pagination(w, r, 100)
		if !ok {
			return
		}
		snapshot, err := importer.Snapshot(r.Context(), hash, r.PathValue("id"), offset, limit)
		if errors.Is(err, ErrImportNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Could not read snapshot", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}))
}

func (a *Auth) session(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		a.failure(w, ErrNoConnection)
		return "", false
	}
	return sessionHash(cookie.Value), true
}

func pagination(w http.ResponseWriter, r *http.Request, max int) (int, int, bool) {
	offset, limit := 0, 50
	var err error
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
	}
	if err != nil || offset < 0 || offset > 100000 {
		http.Error(w, "Invalid offset", http.StatusBadRequest)
		return 0, 0, false
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > max {
		http.Error(w, "Invalid limit", http.StatusBadRequest)
		return 0, 0, false
	}
	return offset, limit, true
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
