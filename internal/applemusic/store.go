package applemusic

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoConnection = errors.New("Apple Music connection missing or expired")

type Connection struct {
	UserToken  string `json:"user_token"`
	Storefront string `json:"storefront"`
}
type Store interface {
	Save(context.Context, string, string, Connection, time.Time) error
	Load(context.Context, string) (Connection, error)
	Delete(context.Context, string) error
}
type PostgresStore struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

func NewPostgresStore(pool *pgxpool.Pool, key []byte) (*PostgresStore, error) {
	if len(key) != 32 {
		return nil, errors.New("token encryption key must contain 32 bytes")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCMWithRandomNonce(b)
	if e != nil {
		return nil, e
	}
	return &PostgresStore{pool: pool, aead: a}, nil
}
func (s *PostgresStore) seal(hash string, c Connection) ([]byte, error) {
	p, e := json.Marshal(c)
	if e != nil {
		return nil, e
	}
	return s.aead.Seal(nil, nil, p, []byte(hash)), nil
}
func (s *PostgresStore) open(hash string, b []byte) (Connection, error) {
	var c Connection
	p, e := s.aead.Open(nil, nil, b, []byte(hash))
	if e != nil {
		return c, errors.New("cannot decrypt Apple Music connection")
	}
	e = json.Unmarshal(p, &c)
	return c, e
}
func (s *PostgresStore) Save(ctx context.Context, hash, old string, c Connection, expires time.Time) error {
	b, e := s.seal(hash, c)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, "DELETE FROM apple_connections WHERE session_hash=$1 OR expires_at<=now()", old); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO apple_connections(session_hash,encrypted_credentials,expires_at) VALUES($1,$2,$3)", hash, b, expires); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PostgresStore) Load(ctx context.Context, hash string) (Connection, error) {
	var b []byte
	e := s.pool.QueryRow(ctx, "SELECT encrypted_credentials FROM apple_connections WHERE session_hash=$1 AND expires_at>now()", hash).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return Connection{}, ErrNoConnection
	}
	if e != nil {
		return Connection{}, e
	}
	return s.open(hash, b)
}
func (s *PostgresStore) Delete(ctx context.Context, hash string) error {
	_, e := s.pool.Exec(ctx, "DELETE FROM apple_connections WHERE session_hash=$1", hash)
	return e
}
