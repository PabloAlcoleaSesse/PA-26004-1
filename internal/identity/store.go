package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserNotFound = errors.New("application_user_not_found")

type User struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type ProviderAccount struct {
	Provider  string    `json:"provider"`
	LinkedAt  time.Time `json:"linked_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// LinkProviderTx rotates a session mapping and links the provider identity in
// the same transaction as encrypted credential persistence.
func LinkProviderTx(ctx context.Context, tx pgx.Tx, sessionHash, oldSessionHash, provider, accountID string, expiresAt time.Time) error {
	var userID string
	if oldSessionHash != "" {
		err := tx.QueryRow(ctx, "SELECT user_id FROM user_sessions WHERE session_hash=$1 FOR UPDATE", oldSessionHash).Scan(&userID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if userID == "" {
		userID = newID()
		if _, err := tx.Exec(ctx, "INSERT INTO app_users(id) VALUES($1)", userID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_sessions(session_hash,user_id,expires_at)
		VALUES($1,$2,$3) ON CONFLICT(session_hash) DO UPDATE SET user_id=excluded.user_id,expires_at=excluded.expires_at`, sessionHash, userID, expiresAt); err != nil {
		return err
	}
	if oldSessionHash != "" && oldSessionHash != sessionHash {
		if _, err := tx.Exec(ctx, "DELETE FROM user_sessions WHERE session_hash=$1", oldSessionHash); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_accounts(user_id,provider,account_id)
		VALUES($1,$2,$3) ON CONFLICT(user_id,provider) DO UPDATE SET account_id=excluded.account_id,updated_at=now()`, userID, provider, accountID); err != nil {
		return err
	}
	return nil
}

// Fingerprint returns a stable opaque identifier for a provider token when a
// provider does not expose a durable account ID. The token itself is never stored.
func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Load(ctx context.Context, sessionHash string) (User, []ProviderAccount, error) {
	var user User
	err := s.pool.QueryRow(ctx, `SELECT u.id,u.created_at FROM app_users u JOIN user_sessions s ON s.user_id=u.id
		WHERE s.session_hash=$1 AND s.expires_at>now()`, sessionHash).Scan(&user.ID, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, ErrUserNotFound
	}
	if err != nil {
		return User{}, nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT provider,linked_at,updated_at FROM provider_accounts WHERE user_id=$1 ORDER BY provider", user.ID)
	if err != nil {
		return User{}, nil, err
	}
	defer rows.Close()
	accounts := []ProviderAccount{}
	for rows.Next() {
		var account ProviderAccount
		if err := rows.Scan(&account.Provider, &account.LinkedAt, &account.UpdatedAt); err != nil {
			return User{}, nil, err
		}
		accounts = append(accounts, account)
	}
	return user, accounts, rows.Err()
}

func newID() string {
	// rand.Text is backed by crypto/rand and yields an opaque, URL-safe ID.
	return randomText()
}

var randomText = func() string {
	// This indirection keeps ID generation replaceable in unit tests.
	return rand.Text()
}
