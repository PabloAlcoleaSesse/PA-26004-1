package spotify

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The fake store enforces the same update serialization as SELECT FOR UPDATE.
// Tests never contact Spotify or require a container/database.
type memoryStore struct {
	mu     sync.Mutex
	rows   map[string]Connection
	expiry map[string]time.Time
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: map[string]Connection{}, expiry: map[string]time.Time{}}
}
func (s *memoryStore) Save(_ context.Context, hash, old string, c Connection, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, old)
	s.rows[hash] = c
	s.expiry[hash] = expires
	return nil
}
func (s *memoryStore) Update(_ context.Context, hash string, f func(*Connection) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[hash]
	if !ok || !s.expiry[hash].After(time.Now()) {
		return ErrNoConnection
	}
	if err := f(&c); err != nil {
		return err
	}
	s.rows[hash] = c
	return nil
}
func (s *memoryStore) Delete(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, hash)
	return nil
}

func testAuth(t *testing.T, provider http.HandlerFunc) (*Auth, *memoryStore, http.Handler) {
	t.Helper()
	server := httptest.NewServer(provider)
	t.Cleanup(server.Close)
	client := NewClient("test-client", "http://127.0.0.1:8080/auth/spotify/callback")
	client.tokenURL = server.URL + "/token"
	client.profileURL = server.URL + "/me"
	store := newMemoryStore()
	auth := NewAuth(client, store)
	mux := http.NewServeMux()
	auth.Register(mux)
	return auth, store, mux
}

func startFlow(t *testing.T, mux http.Handler) (string, *http.Cookie, url.Values) {
	t.Helper()
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest("GET", "/auth/spotify", nil))
	if r.Code != 302 {
		t.Fatalf("start status: %d", r.Code)
	}
	u, err := url.Parse(r.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("missing cookie protection")
	}
	return u.Query().Get("state"), cookies[0], u.Query()
}

func callback(mux http.Handler, query string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/auth/spotify/callback?"+query, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestAuthorizationRoundTrip(t *testing.T) {
	var challenge string
	var tokenCalls atomic.Int32
	_, store, mux := testAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			verifier := r.Form.Get("code_verifier")
			digest := sha256.Sum256([]byte(verifier))
			if len(verifier) < 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
				t.Error("PKCE verifier did not match challenge")
			}
			if r.Form.Get("client_id") != "test-client" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != "http://127.0.0.1:8080/auth/spotify/callback" {
				t.Error("incorrect code exchange")
			}
			fmt.Fprint(w, `{"access_token":"secret-access","refresh_token":"secret-refresh","token_type":"Bearer","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret-access" {
			t.Error("missing bearer token")
		}
		fmt.Fprint(w, `{"account_id":"stable-id","id":"mutable-id","display_name":"Listener"}`)
	})
	state, cookie, params := startFlow(t, mux)
	challenge = params.Get("code_challenge")
	if params.Get("code_challenge_method") != "S256" || strings.Contains(params.Get("scope"), "modify") {
		t.Fatal("incorrect authorization permissions or PKCE")
	}
	// A matching state without the originating browser cookie is rejected.
	if w := callback(mux, "state="+state+"&code=test-code", nil); w.Code != 400 {
		t.Fatal("accepted missing cookie")
	}
	w := callback(mux, "state="+state+"&code=test-code", cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/api/connections/spotify" {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.Value == "" {
		t.Fatal("missing session")
	}
	if _, ok := store.rows[session.Value]; ok {
		t.Fatal("raw session was stored")
	}
	if store.rows[sessionHash(session.Value)].Profile.AccountID != "stable-id" {
		t.Fatal("incorrect account identity")
	}
	request := httptest.NewRequest("GET", "/api/connections/spotify", nil)
	request.AddCookie(session)
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, request)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "stable-id") || strings.Contains(result.Body.String(), "secret-") {
		t.Fatalf("unsafe or incorrect profile: %s", result.Body.String())
	}
	if result.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("profile may be cached")
	}
	if w := callback(mux, "state="+state+"&code=test-code", cookie); w.Code != 400 {
		t.Fatal("callback replay accepted")
	}
	if tokenCalls.Load() != 1 {
		t.Fatal("code exchanged more than once")
	}
}

func TestRejectedFlowsNeverContactSpotify(t *testing.T) {
	for _, kind := range []string{"mismatch", "expired", "denied", "missing-code"} {
		t.Run(kind, func(t *testing.T) {
			auth, store, mux := testAuth(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected provider call"); w.WriteHeader(500) })
			state, cookie, _ := startFlow(t, mux)
			query := "state=" + state + "&code=test-code"
			switch kind {
			case "mismatch":
				cookie.Value = "other-browser"
			case "expired":
				auth.pending[state] = attempt{verifier: "expired", expires: time.Now().Add(-time.Minute)}
			case "denied":
				query = "state=" + state + "&error=access_denied"
			case "missing-code":
				query = "state=" + state
			}
			if w := callback(mux, query, cookie); w.Code != 400 {
				t.Fatalf("status %d", w.Code)
			}
			if len(store.rows) != 0 {
				t.Fatal("saved rejected connection")
			}
		})
	}
}

func TestRefreshSerializedAndRotatedTokenSurvivesProfileFailure(t *testing.T) {
	var refreshes atomic.Int32
	var failProfile atomic.Bool
	failProfile.Store(true)
	_, store, mux := testAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes.Add(1)
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("grant_type") != "refresh_token" {
				t.Error("wrong refresh request")
			}
			fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":3600}`)
			return
		}
		if failProfile.Load() {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"account_id":"stable-id","display_name":"Listener"}`)
	})
	hash := sessionHash("browser")
	_ = store.Save(context.Background(), hash, "", Connection{Profile: Profile{AccountID: "stable-id"}, Tokens: Tokens{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute)}}, time.Now().Add(time.Hour))
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/connections/spotify", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "browser"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := get()
	if w.Code != 429 || w.Header().Get("Retry-After") != "30" {
		t.Fatalf("rate limit not preserved: %d", w.Code)
	}
	if store.rows[hash].Tokens.RefreshToken != "rotated-refresh" {
		t.Fatal("lost rotated token after profile failure")
	}
	// Force expiry again, then concurrent requests must perform only one refresh.
	c := store.rows[hash]
	c.Tokens.RefreshToken = "old-refresh"
	c.Tokens.ExpiresAt = time.Now().Add(-time.Minute)
	store.rows[hash] = c
	failProfile.Store(false)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if w := get(); w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		})
	}
	wg.Wait()
	if refreshes.Load() != 2 {
		t.Fatalf("refreshes: %d", refreshes.Load())
	}
}

func TestSessionIsolationAndDisconnectCSRF(t *testing.T) {
	_, store, mux := testAuth(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected provider call") })
	_ = store.Save(context.Background(), sessionHash("owner"), "", Connection{}, time.Now().Add(time.Hour))
	for _, session := range []string{"", "different-browser"} {
		r := httptest.NewRequest("GET", "/api/connections/spotify", nil)
		if session != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("unauthenticated session accepted")
		}
	}
	for _, origin := range []string{"", "https://attacker.example", "http://127.0.0.1:8080"} {
		r := httptest.NewRequest("DELETE", "/api/connections/spotify", nil)
		r.Header.Set("Origin", origin)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "owner"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		want := 403
		if origin == "http://127.0.0.1:8080" {
			want = 204
		}
		if w.Code != want {
			t.Fatalf("origin %s: %d", origin, w.Code)
		}
	}
	if len(store.rows) != 0 {
		t.Fatal("connection not deleted")
	}
}

func TestEncryptionDetectsTamperingAndSessionSwap(t *testing.T) {
	store, err := NewPostgresStore(nil, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	c := Connection{Tokens: Tokens{AccessToken: "private-access", RefreshToken: "private-refresh"}}
	encrypted, err := store.seal("session-a", c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "private") {
		t.Fatal("plaintext credentials")
	}
	got, err := store.open("session-a", encrypted)
	if err != nil || got.Tokens != c.Tokens {
		t.Fatal("round trip failed")
	}
	if _, err := store.open("session-b", encrypted); err == nil {
		t.Fatal("session swap accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := store.open("session-a", encrypted); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := NewPostgresStore(nil, []byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestProviderRefreshAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"omitted-refresh", `{"access_token":"new","token_type":"Bearer","expires_in":3600}`, 200},
		{"revoked", `{"error":"invalid_grant","error_description":"private-details"}`, 400},
		{"malformed", `{"access_token":"secret"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, _, _ := testAuth(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			tokens, err := auth.client.Refresh(context.Background(), Tokens{RefreshToken: "keep-me", Scope: "keep-scope"})
			if tc.name == "omitted-refresh" {
				if err != nil || tokens.RefreshToken != "keep-me" || tokens.Scope != "keep-scope" {
					t.Fatal("omitted refresh fields not preserved")
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "private-details") || strings.Contains(err.Error(), "secret") {
					t.Fatal("unsafe or missing error")
				}
				if tc.name == "revoked" {
					var p *ProviderError
					if !errors.As(err, &p) || !p.Reconnect {
						t.Fatal("revocation not recognized")
					}
				}
			}
		})
	}
}

func TestUnauthorizedAccessTokenRefreshesOnce(t *testing.T) {
	var refreshes atomic.Int32
	_, store, mux := testAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes.Add(1)
			fmt.Fprint(w, `{"access_token":"replacement","token_type":"Bearer","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer replacement" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"account_id":"stable"}`)
	})
	_ = store.Save(context.Background(), sessionHash("browser"), "", Connection{Profile: Profile{AccountID: "stable"}, Tokens: Tokens{AccessToken: "rejected", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}}, time.Now().Add(time.Hour))
	r := httptest.NewRequest("GET", "/api/connections/spotify", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "browser"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || refreshes.Load() != 1 {
		t.Fatalf("status %d, refreshes %d", w.Code, refreshes.Load())
	}
}

func TestProviderRedirectCannotLeakCredentials(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Store(true) }))
	defer target.Close()
	auth, _, _ := testAuth(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	if _, err := auth.client.Profile(context.Background(), "private-token"); err == nil {
		t.Fatal("accepted redirect")
	}
	if followed.Load() {
		t.Fatal("followed credential-bearing redirect")
	}
}

func TestHTTPSCookiesAndPendingLimit(t *testing.T) {
	auth := NewAuth(NewClient("client", "https://music.example/auth/spotify/callback"), newMemoryStore())
	mux := http.NewServeMux()
	auth.Register(mux)
	_, cookie, _ := startFlow(t, mux)
	if !cookie.Secure {
		t.Fatal("HTTPS cookie missing Secure")
	}
	for i := range 1024 {
		auth.pending[fmt.Sprint(i)] = attempt{expires: time.Now().Add(time.Minute)}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/spotify", nil))
	if w.Code != 429 {
		t.Fatal("pending flows not bounded")
	}
	for key := range auth.pending {
		auth.pending[key] = attempt{expires: time.Now().Add(-time.Minute)}
	}
	startFlow(t, mux)
	if len(auth.pending) != 1 {
		t.Fatal("expired flows not removed")
	}
}
