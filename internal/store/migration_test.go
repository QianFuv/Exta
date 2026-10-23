package store

import (
	"path/filepath"
	"strings"
	"testing"
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
