package spotify

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

var ErrNoConnection = errors.New("Spotify session missing or expired")

type Connection struct {
	Profile Profile `json:"profile"`
	Tokens  Tokens  `json:"tokens"`
}

// Store separates OAuth from persistence. Update must serialize changes to the
// same session so concurrent requests cannot rotate a refresh token twice.
type Store interface {
	Save(context.Context, string, string, Connection, time.Time) error
	Update(context.Context, string, func(*Connection) error) error
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
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// Go generates and prepends a fresh random nonce for every encryption.
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &PostgresStore{pool: pool, aead: aead}, nil
}

func (s *PostgresStore) seal(hash string, connection Connection) ([]byte, error) {
	plain, err := json.Marshal(connection)
	if err != nil {
		return nil, err
	}
	// Associated data prevents swapping ciphertext between different sessions.
	return s.aead.Seal(nil, nil, plain, []byte(hash)), nil
}

func (s *PostgresStore) open(hash string, encrypted []byte) (Connection, error) {
	var connection Connection
	plain, err := s.aead.Open(nil, nil, encrypted, []byte(hash))
	if err != nil {
		return connection, errors.New("cannot decrypt Spotify connection")
	}
	if err := json.Unmarshal(plain, &connection); err != nil {
		return connection, errors.New("invalid stored Spotify connection")
	}
	return connection, nil
}

// Save atomically rotates the local session after successful authorization.
func (s *PostgresStore) Save(ctx context.Context, hash, oldHash string, connection Connection, expires time.Time) error {
	encrypted, err := s.seal(hash, connection)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "DELETE FROM spotify_connections WHERE session_hash = $1 OR expires_at <= now()", oldHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO spotify_connections (session_hash, encrypted_credentials, expires_at) VALUES ($1, $2, $3)", hash, encrypted, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Update(ctx context.Context, hash string, update func(*Connection) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var encrypted []byte
	// A PostgreSQL row lock coordinates refreshes across processes. The request
	// deadline bounds both lock acquisition and the provider calls inside it.
	err = tx.QueryRow(ctx, "SELECT encrypted_credentials FROM spotify_connections WHERE session_hash = $1 AND expires_at > now() FOR UPDATE", hash).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoConnection
	}
	if err != nil {
		return err
	}
	connection, err := s.open(hash, encrypted)
	if err != nil {
		return err
	}
	if err := update(&connection); err != nil {
		return err
	}
	encrypted, err = s.seal(hash, connection)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE spotify_connections SET encrypted_credentials = $2, updated_at = now() WHERE session_hash = $1", hash, encrypted); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Delete(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM spotify_connections WHERE session_hash = $1", hash)
	return err
}
