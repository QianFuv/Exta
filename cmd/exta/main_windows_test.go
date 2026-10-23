package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"exta/internal/security"
)

func TestMain(tests *testing.M) {
	if err := security.PrepareProcessOwnership(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(tests.Run())
}

func TestLiveRuntimeFilesRemainUserOwned(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	state, _, _, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer state.Close()
	if _, err := state.DB.ExecContext(t.Context(), "UPDATE activity_fence SET generation=generation WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"exta.lock", "master.key", "config.json", "exta.sqlite", "exta.sqlite-wal", "exta.sqlite-shm"} {
		if err := security.CheckPrivate(filepath.Join(directory, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
