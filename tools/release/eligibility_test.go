package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseEligibilityReadsCurrentSourceBundle(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cmd", "tests", "tools", "api", "web"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum", ".node-version", ".npmrc"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(root, "internal", "webassets")
	if err := os.MkdirAll(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	writeBundle := func(value string) {
		t.Helper()
		file, err := os.Create(filepath.Join(bundle, "production.zip"))
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		entry, err := writer.Create("bundle/manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writeBundle(`{"format":1,"mode":"backend-only","files":{},"routes":[]}`)
	t.Chdir(root)
	for _, name := range []string{".node-version", ".npmrc"} {
		before, err := sourceFiles()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		after, err := sourceFiles()
		if err != nil {
			t.Fatal(err)
		}
		if before[name] == after[name] {
			t.Fatal("toolchain/config input escaped post-build snapshot")
		}
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := run([]string{"verify", "--output", filepath.Join(root, "release")})
	if err == nil || !strings.Contains(err.Error(), "production release requires") {
		t.Fatalf("source gate: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "release")); !os.IsNotExist(err) {
		t.Fatal("ineligible source produced release output")
	}
	writeBundle(`{"format":1,"mode":"production","files":{},"routes":[]}`)
	err = run([]string{"verify", "--backend-only", "--output", filepath.Join(root, "release")})
	if err == nil || !strings.Contains(err.Error(), "production bundle requires") {
		t.Fatalf("corrupt source gate: %v", err)
	}
}
