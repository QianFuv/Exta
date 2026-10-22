package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseEligibilityReadsCurrentSourceBundle(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cmd", "tests", "tools", "api"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bundle := filepath.Join(root, "internal", "webassets", "bundle")
	if err := os.MkdirAll(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"format":1,"mode":"backend-only","files":{},"routes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	err := run([]string{"verify", "--output", filepath.Join(root, "release")})
	if err == nil || !strings.Contains(err.Error(), "production release requires") {
		t.Fatalf("source gate: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "release")); !os.IsNotExist(err) {
		t.Fatal("ineligible source produced release output")
	}
	if err = os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"format":1,"mode":"production","files":{},"routes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"verify", "--backend-only", "--output", filepath.Join(root, "release")})
	if err == nil || !strings.Contains(err.Error(), "production bundle requires") {
		t.Fatalf("corrupt source gate: %v", err)
	}
}
