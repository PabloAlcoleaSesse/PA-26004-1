package platform

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// MigrateApp applies numbered application migrations separately from River's
// schema. A transaction and advisory lock serialize concurrent deploy commands.
// Checksums catch accidental edits to migrations that have already been applied.
func MigrateApp(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(260041)"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_migrations (
		name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		checksum := hex.EncodeToString(digest[:])
		var previous string
		err = tx.QueryRow(ctx, "SELECT checksum FROM app_migrations WHERE name = $1", name).Scan(&previous)
		if err == nil {
			if previous != checksum {
				return fmt.Errorf("applied migration was modified: %s", name)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO app_migrations (name, checksum) VALUES ($1, $2)", name, checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
