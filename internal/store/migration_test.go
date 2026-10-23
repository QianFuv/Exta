package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrationIdentityIgnoresOnlyLineEndings(t *testing.T) {
	original := initialSchema
	defer func() { initialSchema = original }()
	initialSchema = strings.ReplaceAll(original, "\r\n", "\n")
	path := filepath.Join(t.TempDir(), "state.sqlite")
	state, err := Open(t.Context(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	state.Close()
	initialSchema = strings.ReplaceAll(initialSchema, "\n", "\r\n")
	state, err = Open(t.Context(), path, nil)
	if err != nil {
		t.Fatal("same migration rejected after line-ending conversion", err)
	}
	state.Close()
	initialSchema += "\n-- substantively changed migration\n"
	if state, err = Open(t.Context(), path, nil); err == nil {
		state.Close()
		t.Fatal("substantive migration drift accepted")
	}
}

type migrationTestVault struct{}

func (migrationTestVault) Seal(entity string, plain []byte) ([]byte, error) {
	return append([]byte(entity+":"), plain...), nil
}

func (migrationTestVault) Open(entity string, cipher []byte) ([]byte, error) {
	prefix := entity + ":"
	if !strings.HasPrefix(string(cipher), prefix) {
		return nil, errors.New("synthetic authentication failed")
	}
	return cipher[len(prefix):], nil
}

func TestKeyMigrationPublicFailureCleansUpTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	vault := migrationTestVault{}
	state, err := Open(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.BootstrapAdmin(t.Context(), "synthetic-migration-administrator"); err != nil {
		t.Fatal(err)
	}
	state.Close()
	before, err := InspectExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = Snapshot(t.Context(), path, backup); err != nil {
		t.Fatal(err)
	}
	if verified, err := InspectExisting(t.Context(), backup, vault); err != nil || verified != before {
		t.Fatal("backup verification", err)
	}
	original := keySchema
	defer func() { keySchema = original }()
	keySchema += "\nSELECT * FROM deliberately_missing_migration_table;"
	if err = MigrateKeySchema(t.Context(), path, vault); err == nil || !strings.Contains(err.Error(), "deliberately_missing_migration_table") {
		t.Fatal("wrong failure boundary", err)
	}
	after, err := InspectExisting(t.Context(), path, vault)
	if err != nil || before != after {
		t.Fatalf("automatic rollback failed: %+v %+v %v", before, after, err)
	}
}

func TestKeyMigrationRollsBackAfterTableRebuild(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err = database.Exec(initialSchema + "\nPRAGMA user_version=1;"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO provider_accounts VALUES('old','exa','{}',1,'original')"); err != nil {
		t.Fatal(err)
	}
	original := keySchema
	defer func() { keySchema = original }()
	keySchema += "\nSELECT * FROM deliberately_missing_migration_table;"
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrateKeySchemaTx(t.Context(), transaction, 0, time.Now()); err == nil || !strings.Contains(err.Error(), "deliberately_missing_migration_table") {
		t.Fatal("did not reach injected failure", err)
	}
	var rebuilt int
	if err = transaction.QueryRow("SELECT count(*) FROM legacy_provider_accounts WHERE id='old'").Scan(&rebuilt); err != nil || rebuilt != 1 {
		t.Fatal("test did not reach completed table rebuild", err)
	}
	if err = transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count, version int
	if err = database.QueryRow("SELECT count(*) FROM provider_accounts WHERE id='old'").Scan(&count); err != nil || count != 1 {
		t.Fatal("old records lost on rollback", err)
	}
	if err = database.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("schema advanced on failure", err)
	}
	if err = validateSchemaObjects(context.Background(), database, 1); err != nil {
		t.Fatal("schema rollback incomplete", err)
	}
}
