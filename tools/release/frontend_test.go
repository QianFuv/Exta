package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendProvenanceRejectsSourceAndLicenseDrift(t *testing.T) {
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"web/src", "tools/embed", "tools/embedpack", frontendRoot, "web/index.html", "web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/tsconfig.json", "api/contract-manifest.json", ".node-version", ".npmrc"} {
		err := filepath.WalkDir(filepath.Join(source, name), func(filename string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, filename)
			if err != nil {
				return err
			}
			target := filepath.Join(root, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			data, err := os.ReadFile(filename)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	archive, err := openFrontend()
	if err != nil {
		t.Fatal(err)
	}
	evidenceRoot := filepath.Join(root, "evidence")
	if err := fs.WalkDir(archive, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(evidenceRoot, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := fs.ReadFile(archive, name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	evidenceFiles := os.DirFS(evidenceRoot)
	data, err := fs.ReadFile(evidenceFiles, "provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence frontendEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if err := verifyFrontendFiles(evidenceFiles, evidence.ContractSHA256); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path, content, expected string }{
		{"source", "web/src/main.tsx", "changed", "stale frontend source"},
		{"npm config", ".npmrc", "changed", "stale frontend source"},
		{"node version", ".node-version", "changed", "stale frontend source"},
		{"new source", "web/src/unbuilt.tsx", "export const unbuilt=true", "source set changed"},
		{"license", "", "corrupt", "license integrity mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := test.path
			if name == "" {
				for file := range evidence.Dependencies[0].Files {
					name = filepath.Join(evidenceRoot, "licenses", filepath.FromSlash(file))
					break
				}
			}
			original, readErr := os.ReadFile(name)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
			if err := os.WriteFile(name, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if os.IsNotExist(readErr) {
					if err := os.Remove(name); err != nil {
						t.Error(err)
					}
				} else if err := os.WriteFile(name, original, 0600); err != nil {
					t.Error(err)
				}
			})
			if err := verifyFrontendFiles(evidenceFiles, evidence.ContractSHA256); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("drift accepted or wrong failure: %v", err)
			}
		})
	}
}
