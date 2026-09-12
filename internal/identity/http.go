package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
)

func RegisterHTTP(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("music_session")
		if err != nil || cookie.Value == "" {
			http.Error(w, "Connect a music provider first", http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256([]byte(cookie.Value))
		user, accounts, err := store.Load(r.Context(), hex.EncodeToString(sum[:]))
		if errors.Is(err, ErrUserNotFound) {
			http.Error(w, "Connect a music provider first", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "Could not load application user", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			User     User              `json:"user"`
			Accounts []ProviderAccount `json:"accounts"`
		}{user, accounts})
	})
}
