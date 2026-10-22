package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestArchivePreservesHiddenAssetsAndDeterministicBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "input")
	if err := os.MkdirAll(filepath.Join(source, "bundle/assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"_logo.svg", ".hidden.js"} {
		if err := os.WriteFile(filepath.Join(source, "bundle/assets", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, second := filepath.Join(root, "first.zip"), filepath.Join(root, "second.zip")
	if err := pack(source, first); err != nil {
		t.Fatal(err)
	}
	if err := pack(source, second); err != nil {
		t.Fatal(err)
	}
	one, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatal("nondeterministic archive")
	}
	archive, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"_logo.svg", ".hidden.js"} {
		value, err := fs.ReadFile(archive, "bundle/assets/"+name)
		if err != nil || string(value) != "fixture" {
			t.Fatalf("missing asset: %v", err)
		}
	}
}
