//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestServeRejectsWorldReadableMasterKey(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", &output); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(directory, "master.key"), 0644); err != nil {
		t.Fatal(err)
	}
	state, _, _, lock, err := openRuntime(t.Context(), directory)
	if err == nil {
		state.Close()
		lock.Close()
		t.Fatal("serve accepted world-readable master key")
	}
}

func TestInitRejectsExistingInsecureDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := initialize(t.Context(), directory, "synthetic-administrator-password", new(bytes.Buffer)); err == nil {
		t.Fatal("insecure existing object rehabilitated in place")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected init wrote secret state")
	}
}
