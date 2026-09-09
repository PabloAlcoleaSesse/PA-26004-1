package spotify

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PabloAlcoleaSesse/PA-26004-1/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This opt-in test uses a unique schema and removes only that schema afterward.
// TEST_DATABASE_URL must point to a development/test database, never production.
func TestPostgresConnectionStore(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "spotify_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE") }()
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Applying twice verifies migration tracking rather than IF NOT EXISTS alone.
	for range 2 {
		if err := platform.MigrateApp(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewPostgresStore(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	hash := sessionHash("browser-session")
	c := Connection{Profile: Profile{AccountID: "stable"}, Tokens: Tokens{AccessToken: "private-access", RefreshToken: "private-refresh", Scope: "0"}}
	if err := store.Save(ctx, hash, "", c, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := pool.QueryRow(ctx, "SELECT encrypted_credentials FROM spotify_connections WHERE session_hash = $1", hash).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "private") {
		t.Fatal("plaintext credentials stored")
	}
	// A newly constructed store can read credentials after an API restart.
	restarted, _ := NewPostgresStore(pool, make([]byte, 32))
	if err := restarted.Update(ctx, hash, func(got *Connection) error {
		if got.Tokens.RefreshToken != "private-refresh" {
			t.Error("credentials did not survive restart")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Concurrent read-modify-write operations must not lose updates.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := store.Update(ctx, hash, func(c *Connection) error {
				n, _ := strconv.Atoi(c.Tokens.Scope)
				c.Tokens.Scope = strconv.Itoa(n + 1)
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := store.Update(ctx, hash, func(c *Connection) error {
		if c.Tokens.Scope != "8" {
			t.Errorf("lost concurrent update: %s", c.Tokens.Scope)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A failed operation must not commit partially modified credentials.
	sentinel := errors.New("abort")
	if err := store.Update(ctx, hash, func(c *Connection) error { c.Tokens.RefreshToken = "wrong"; return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := store.Update(ctx, hash, func(c *Connection) error {
		if c.Tokens.RefreshToken != "private-refresh" {
			t.Error("rollback failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	newHash := sessionHash("rotated-session")
	if err := store.Save(ctx, newHash, hash, c, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, hash, func(*Connection) error { return nil }); !errors.Is(err, ErrNoConnection) {
		t.Fatal("old session still valid")
	}
	if _, err := pool.Exec(ctx, "UPDATE spotify_connections SET expires_at = now() - interval '1 minute' WHERE session_hash = $1", newHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, newHash, func(*Connection) error { return nil }); !errors.Is(err, ErrNoConnection) {
		t.Fatal("expired session accepted")
	}
	if err := store.Delete(ctx, newHash); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM spotify_connections").Scan(&count); err != nil || count != 0 {
		t.Fatal("disconnect did not delete credentials")
	}
}
