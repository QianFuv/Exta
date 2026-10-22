//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestServeRejectsWorldReadableMasterKey(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := initialize(t.Context(), directory, &output); err != nil {
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
