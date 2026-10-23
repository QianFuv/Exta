package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"strings"
	"time"
)

//go:embed migrations/002.sql
var keySchema string

func schemaDigest(source string) string {
	return Digest([]byte(strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")))
}

// MigrateKeySchema converts an exclusively locked, verified schema-1 database.
// The caller must first create and verify a backup; this function never opens
// runtime workers, changes configuration, or dispatches upstream requests.
func MigrateKeySchema(ctx context.Context, path string, vault Vault) error {
	inspection, err := InspectExisting(ctx, path, vault)
	if err != nil {
		return err
	}
	if !inspection.Unlocked {
		return errors.New("migration requires authenticated retained ciphertext")
	}
	if inspection.SchemaVersion == 2 {
		return nil
	}
	database, err := openInspection(path, "rw")
	if err != nil {
		return err
	}
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	// SQLite's documented table-rebuild procedure disables FK enforcement before
	// the transaction and checks every reference before committing the rebuild.
	if _, err = connection.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err = migrateKeySchemaTx(ctx, transaction, inspection.Generation, time.Now()); err != nil {
		return err
	}
	return transaction.Commit()
}

func migrateKeySchemaTx(ctx context.Context, transaction *sql.Tx, generation int64, now time.Time) error {
	var version int
	var currentGeneration int64
	if err := transaction.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if err := transaction.QueryRowContext(ctx, "SELECT generation FROM activity_fence WHERE id=1").Scan(&currentGeneration); err != nil {
		return err
	}
	if version != 1 || currentGeneration != generation {
		return errors.New("database changed after migration preflight")
	}
	if _, err := transaction.ExecContext(ctx, keySchema); err != nil {
		return err
	}
	rows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	hasViolation := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if hasViolation {
		return errors.New("key migration would break retained references")
	}
	if err = AdvanceActivityTx(ctx, transaction); err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(2,?,?)", schemaDigest(keySchema), Stamp(now)); err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, "PRAGMA user_version=2")
	return err
}
