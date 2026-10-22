package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"exta/internal/security"
	"exta/internal/store"
)

func TestSnapshotIncludesCommittedWALAndQuotedPaths(t *testing.T) {
	state, owner := openStore(t)
	addAccount(t, state, owner, 1000000)
	info, err := os.Stat(state.Path + "-wal")
	if err != nil || info.Size() == 0 {
		t.Fatalf("fixture did not retain WAL: %v", err)
	}
	before, err := store.InspectExisting(t.Context(), state.Path, state.Vault)
	if err != nil || !before.Unlocked {
		t.Fatalf("inspect %v %+v", err, before)
	}
	destination := filepath.Join(t.TempDir(), "snapshot ' 测试.sqlite")
	if err = store.Snapshot(t.Context(), state.Path, destination); err != nil {
		t.Fatal(err)
	}
	after, err := store.InspectExisting(t.Context(), destination, state.Vault)
	if err != nil || before != after {
		t.Fatalf("snapshot lost WAL/identity: %v before=%+v after=%+v", err, before, after)
	}
	if err = store.Snapshot(t.Context(), state.Path, destination); err == nil {
		t.Fatal("overwrote existing snapshot")
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error { return store.AdvanceActivityTx(t.Context(), tx) }); err != nil {
		t.Fatal(err)
	}
	path, vault := state.Path, state.Vault
	state.Close()
	if err = store.RestoreSnapshot(t.Context(), path, destination); err != nil {
		t.Fatal(err)
	}
	result, err := store.InspectExisting(t.Context(), path, vault)
	if err != nil || result != before {
		t.Fatalf("restore did not restore logical state: %v %+v", err, result)
	}
}

func TestInspectionNeverInitializesOrRepairsState(t *testing.T) {
	state, _ := openStore(t)
	wrong, _ := security.NewVault(bytes.Repeat([]byte{5}, 32))
	result, err := store.InspectExisting(t.Context(), state.Path, wrong)
	if err != nil || result.Unlocked {
		t.Fatalf("wrong key accepted %v %+v", err, result)
	}
	if _, err = state.DB.Exec("DELETE FROM secret_metadata"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.InspectExisting(t.Context(), state.Path, state.Vault); err == nil {
		t.Fatal("missing sentinel repaired")
	}
	var count int
	if err = state.DB.QueryRow("SELECT count(*) FROM secret_metadata").Scan(&count); err != nil || count != 0 {
		t.Fatal("inspection wrote sentinel")
	}
	absent := filepath.Join(t.TempDir(), "absent.sqlite")
	if _, err = store.InspectExisting(t.Context(), absent, state.Vault); err == nil {
		t.Fatal("inspection initialized absent database")
	}
	if _, err = os.Stat(absent); !os.IsNotExist(err) {
		t.Fatal("inspection created database")
	}
}

func TestInspectionRejectsSchemaDriftAndDamagedCiphertext(t *testing.T) {
	for _, mutation := range []string{"DROP INDEX research_upstream", "PRAGMA user_version=2", "UPDATE schema_migrations SET checksum='changed'", "UPDATE secret_metadata SET ciphertext=x'0000'", "CREATE TRIGGER sqliteXfreeze AFTER UPDATE OF generation ON activity_fence WHEN NEW.generation>OLD.generation BEGIN UPDATE activity_fence SET generation=OLD.generation WHERE id=1; END"} {
		t.Run(mutation, func(t *testing.T) {
			state, _ := openStore(t)
			if _, err := state.DB.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			result, err := store.InspectExisting(t.Context(), state.Path, state.Vault)
			if err == nil && result.Unlocked {
				t.Fatal("unsafe database accepted")
			}
		})
	}
}

func TestActivityFenceFailsClosedAtMissingInvalidAndExhaustedValues(t *testing.T) {
	for _, mutation := range []string{"DELETE FROM activity_fence", "UPDATE activity_fence SET generation=-1", "UPDATE activity_fence SET generation=1.5", "UPDATE activity_fence SET generation=9223372036854775807"} {
		t.Run(mutation, func(t *testing.T) {
			state, _ := openStore(t)
			if _, err := state.DB.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := state.Write(t.Context(), func(tx *sql.Tx) error { return store.AdvanceActivityTx(t.Context(), tx) }); err == nil {
				t.Fatal("damaged activity fence advanced")
			}
		})
	}
}

func TestCancelledSnapshotPreservesDestinationAsIncomplete(t *testing.T) {
	state, _ := openStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "interrupted.sqlite")
	if err := store.Snapshot(ctx, state.Path, path); err == nil {
		t.Fatal("cancelled snapshot succeeded")
	}
	if _, err := store.InspectExisting(t.Context(), path, state.Vault); err == nil {
		t.Fatal("incomplete snapshot accepted")
	}
}
