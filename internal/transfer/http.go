package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const sessionCookie = "music_session"

func RegisterHTTP(mux *http.ServeMux, service *Service, queue *river.Client[pgx.Tx], origin string) {
	mux.HandleFunc("POST /api/transfers/previews", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			SourceProvider      string `json:"source_provider"`
			SourceSnapshotID    string `json:"source_snapshot_id"`
			DestinationProvider string `json:"destination_provider"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
			http.Error(w, "invalid preview request", http.StatusBadRequest)
			return
		}
		preview, err := service.CreatePreview(r.Context(), hash, input.SourceProvider, input.SourceSnapshotID, input.DestinationProvider)
		if errors.Is(err, ErrInvalidProvider) {
			http.Error(w, "unsupported provider", http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrSourceSnapshotNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not create transfer preview", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusCreated, preview)
	})
	mux.HandleFunc("GET /api/transfers/previews/{id}", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		offset, limit, ok := pagination(w, r, 500)
		if !ok {
			return
		}
		preview, err := service.Preview(r.Context(), hash, r.PathValue("id"), offset, limit)
		if errors.Is(err, ErrPreviewNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not load transfer preview", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
	mux.HandleFunc("POST /api/transfers/previews/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		run, err := service.EnqueueRun(r.Context(), queue, hash, r.PathValue("id"))
		if errors.Is(err, ErrPreviewNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not enqueue transfer", http.StatusInternalServerError)
			return
		}
		location := "/api/transfers/runs/" + run.ID
		w.Header().Set("Location", location)
		writeJSON(w, http.StatusAccepted, map[string]string{"run_id": run.ID, "status_url": location})
	})
	mux.HandleFunc("GET /api/transfers/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		status, err := service.RunStatus(r.Context(), hash, r.PathValue("id"))
		if errors.Is(err, ErrRunNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not load transfer run", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/syncs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		var input struct {
			SourceProvider        string `json:"source_provider"`
			SourcePlaylistID      string `json:"source_playlist_id"`
			DestinationProvider   string `json:"destination_provider"`
			DestinationPlaylistID string `json:"destination_playlist_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
			http.Error(w, "invalid sync request", http.StatusBadRequest)
			return
		}
		status, err := service.EnqueueSync(r.Context(), queue, hash, input.SourceProvider, input.SourcePlaylistID, input.DestinationProvider, input.DestinationPlaylistID)
		if errors.Is(err, ErrInvalidProvider) {
			http.Error(w, "unsupported provider or invalid playlist", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "could not enqueue sync", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Location", "/api/syncs/"+status.ID)
		writeJSON(w, http.StatusAccepted, status)
	})
	mux.HandleFunc("GET /api/syncs/{id}", func(w http.ResponseWriter, r *http.Request) {
		hash, ok := sessionHashFromRequest(w, r)
		if !ok {
			return
		}
		status, err := service.SyncStatus(r.Context(), hash, r.PathValue("id"))
		if errors.Is(err, ErrSyncNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not load sync", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func sessionHashFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Connect a music provider first", http.StatusUnauthorized)
		return "", false
	}
	return hashSession(cookie.Value), true
}

func hashSession(session string) string {
	sum := sha256.Sum256([]byte(session))
	return hex.EncodeToString(sum[:])
}

func pagination(w http.ResponseWriter, r *http.Request, max int) (int, int, bool) {
	offset, limit := 0, 100
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
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
