package spotify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const flowCookie = "spotify_oauth_state"
const sessionCookie = "music_session"
const sessionLifetime = 30 * 24 * time.Hour

type attempt struct {
	verifier string
	expires  time.Time
}

type Auth struct {
	client  *Client
	store   Store
	origin  string
	secure  bool
	mu      sync.Mutex
	pending map[string]attempt
}

// NewAuth expects the redirect URI to have passed config validation.
func NewAuth(client *Client, store Store) *Auth {
	u, _ := url.Parse(client.redirectURI)
	return &Auth{client: client, store: store, origin: u.Scheme + "://" + u.Host, secure: u.Scheme == "https", pending: make(map[string]attempt)}
}

func (a *Auth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/spotify", a.wrap(a.start))
	mux.HandleFunc("GET /auth/spotify/callback", a.wrap(a.callback))
	mux.HandleFunc("GET /api/connections/spotify", a.wrap(a.connection))
	mux.HandleFunc("DELETE /api/connections/spotify", a.wrap(a.disconnect))
}

func (a *Auth) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Finish the whole operation before the HTTP server's write timeout.
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

func (a *Auth) cookie(w http.ResponseWriter, name, value, path string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, MaxAge: age, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
}

func (a *Auth) start(w http.ResponseWriter, r *http.Request) {
	state := rand.Text()
	// Two random strings satisfy PKCE's 43-128 character verifier requirement.
	verifier := rand.Text() + rand.Text()
	now := time.Now()
	a.mu.Lock()
	for key, flow := range a.pending {
		if !flow.expires.After(now) {
			delete(a.pending, key)
		}
	}
	// Abandoned authorization attempts must not consume unbounded memory.
	if len(a.pending) >= 1024 {
		a.mu.Unlock()
		http.Error(w, "Try connecting again later", http.StatusTooManyRequests)
		return
	}
	a.pending[state] = attempt{verifier: verifier, expires: now.Add(10 * time.Minute)}
	a.mu.Unlock()
	a.cookie(w, flowCookie, state, "/auth/spotify", 600)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	http.Redirect(w, r, a.client.AuthorizationURL(state, challenge), http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(flowCookie)
	// State protects against login CSRF; the cookie binds the flow to this
	// browser. Validate both before consuming the single-use authorization.
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		http.Error(w, "Invalid authorization state", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	flow, exists := a.pending[state]
	delete(a.pending, state)
	a.mu.Unlock()
	a.cookie(w, flowCookie, "", "/auth/spotify", -1)
	if !exists || !flow.expires.After(time.Now()) {
		http.Error(w, "Authorization expired or already used", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Error(w, "Spotify authorization was not granted", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}
	tokens, err := a.client.Exchange(r.Context(), code, flow.verifier)
	if err != nil {
		a.failure(w, err)
		return
	}
	profile, err := a.client.Profile(r.Context(), tokens.AccessToken)
	if err != nil {
		a.failure(w, err)
		return
	}
	session := rand.Text()
	oldHash := ""
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		oldHash = sessionHash(cookie.Value)
	}
	if err := a.store.Save(r.Context(), sessionHash(session), oldHash, Connection{Profile: profile, Tokens: tokens}, time.Now().Add(sessionLifetime)); err != nil {
		http.Error(w, "Could not save Spotify connection", http.StatusInternalServerError)
		return
	}
	a.cookie(w, sessionCookie, session, "/", int(sessionLifetime.Seconds()))
	// Redirect removes the authorization code from the browser's current URL.
	http.Redirect(w, r, "/api/connections/spotify", http.StatusSeeOther)
}

func sessionHash(session string) string {
	digest := sha256.Sum256([]byte(session))
	return hex.EncodeToString(digest[:])
}

func (a *Auth) connection(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		a.failure(w, ErrNoConnection)
		return
	}
	var profile Profile
	err = a.client.withConnection(r.Context(), a.store, sessionHash(cookie.Value), func(connection *Connection) error {
		p, err := a.client.Profile(r.Context(), connection.Tokens.AccessToken)
		if err != nil {
			return err
		}
		if p.AccountID != connection.Profile.AccountID {
			return errors.New("Spotify account identity changed")
		}
		connection.Profile = p
		profile = p
		return nil
	})
	if err != nil {
		a.failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Connected bool    `json:"connected"`
		Profile   Profile `json:"profile"`
	}{Connected: true, Profile: profile})
}

func (a *Auth) disconnect(w http.ResponseWriter, r *http.Request) {
	// Cookie authentication needs CSRF protection on mutations. Compare against
	// the configured public origin, never untrusted Host or proxy headers.
	if r.Header.Get("Origin") != a.origin {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		if err := a.store.Delete(r.Context(), sessionHash(cookie.Value)); err != nil {
			http.Error(w, "Could not disconnect Spotify", http.StatusInternalServerError)
			return
		}
	}
	a.cookie(w, sessionCookie, "", "/", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) failure(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNoConnection) {
		http.Error(w, "Connect Spotify first", http.StatusUnauthorized)
		return
	}
	var upstream *ProviderError
	if errors.As(err, &upstream) {
		if upstream.Reconnect || upstream.Status == http.StatusUnauthorized {
			http.Error(w, "Reconnect Spotify", http.StatusUnauthorized)
			return
		}
		if upstream.Status == http.StatusTooManyRequests {
			if upstream.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(upstream.RetryAfter))
			}
			http.Error(w, "Spotify rate limit reached; try again later", http.StatusTooManyRequests)
			return
		}
		if upstream.Status == http.StatusForbidden {
			http.Error(w, "Spotify access denied; check app permissions and allowed users", http.StatusForbidden)
			return
		}
	}
	// Internal errors may contain private data and must not be serialized.
	http.Error(w, "Spotify connection could not be checked", http.StatusBadGateway)
}
