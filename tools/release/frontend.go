package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const frontendRoot = "internal/webassets/production.zip"

type frontendEvidence struct {
	Format         int               `json:"format"`
	Node           string            `json:"node"`
	NPM            string            `json:"npm"`
	Vite           string            `json:"vite"`
	ContractSHA256 string            `json:"contract_sha256"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	Sources        map[string]string `json:"source_files"`
	Dependencies   []struct {
		Name      string            `json:"name"`
		Version   string            `json:"version"`
		Integrity string            `json:"integrity"`
		Files     map[string]string `json:"files"`
	} `json:"dependencies"`
}

func frontendSources() (map[string]string, error) {
	names := []string{"web/index.html", "web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/tsconfig.json", "api/contract-manifest.json", ".node-version", ".npmrc"}
	for _, root := range []string{"web/src", "tools/embed", "tools/embedpack"} {
		if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return errors.New("frontend source link")
			}
			if !entry.IsDir() {
				names = append(names, filepath.ToSlash(name))
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	result := map[string]string{}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if isFrontendText(name) {
			if !utf8.Valid(data) {
				return nil, errors.New("invalid UTF-8 frontend text source")
			}
			data = []byte(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"))
		}
		digest := sha256.Sum256(data)
		result[name] = hex.EncodeToString(digest[:])
	}
	return result, nil
}

func verifyFrontend(contractHash string) error {
	source, err := openFrontend()
	if err != nil {
		return err
	}
	return verifyFrontendFiles(source, contractHash)
}

func verifyFrontendFiles(source fs.FS, contractHash string) error {
	data, err := fs.ReadFile(source, "provenance.json")
	if err != nil {
		return err
	}
	var evidence frontendEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return err
	}
	if evidence.Format != 1 || evidence.Node == "" || evidence.NPM == "" || evidence.Vite == "" || evidence.ContractSHA256 != contractHash || len(evidence.Dependencies) == 0 {
		return errors.New("invalid frontend provenance")
	}
	hash, err := frontendDigest(source, "bundle/manifest.json")
	if err != nil {
		return err
	}
	if hash != evidence.ManifestSHA256 {
		return errors.New("frontend provenance manifest mismatch")
	}
	sources, err := frontendSources()
	if err != nil {
		return err
	}
	if len(sources) != len(evidence.Sources) {
		return errors.New("frontend source set changed; rebuild UI")
	}
	for name, hash := range sources {
		if evidence.Sources[name] != hash {
			return fmt.Errorf("stale frontend source: %s; rebuild UI", name)
		}
	}
	var contract struct {
		Deferred bool              `json:"frontend_deferred"`
		Files    map[string]string `json:"files"`
	}
	data, err = os.ReadFile("api/contract-manifest.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &contract); err != nil {
		return err
	}
	if contract.Deferred {
		return errors.New("frontend contract generation is deferred")
	}
	for name, hash := range contract.Files {
		if strings.HasPrefix(name, "web/") && sources[name] != hash {
			return errors.New("frontend generated contract mismatch")
		}
	}
	licenseFiles := map[string]bool{}
	for _, dependency := range evidence.Dependencies {
		if dependency.Name == "" || dependency.Version == "" || dependency.Integrity == "" || len(dependency.Files) == 0 {
			return errors.New("incomplete shipped dependency evidence")
		}
		for name, expected := range dependency.Files {
			if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") || licenseFiles[name] {
				return errors.New("invalid frontend license path")
			}
			licenseFiles[name] = true
			hash, err := frontendDigest(source, "licenses/"+name)
			if err != nil {
				return err
			}
			if hash != expected {
				return errors.New("frontend license integrity mismatch")
			}
		}
	}
	return fs.WalkDir(source, "licenses", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("frontend license link")
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel("licenses", name)
		if err != nil {
			return err
		}
		if !licenseFiles[filepath.ToSlash(relative)] {
			return errors.New("unmanifested frontend license")
		}
		return nil
	})
}

func copyFrontendEvidence(destination string) error {
	source, err := openFrontend()
	if err != nil {
		return err
	}
	data, err := fs.ReadFile(source, "provenance.json")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(destination, "frontend-provenance.json"), data, 0600); err != nil {
		return err
	}
	return fs.WalkDir(source, "licenses", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel("licenses", name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, "frontend-licenses", relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}

func isFrontendText(name string) bool {
	if name == ".node-version" || name == ".npmrc" {
		return true
	}
	switch filepath.Ext(name) {
	case ".ts", ".tsx", ".js", ".mjs", ".json", ".css", ".html", ".svg", ".go":
		return true
	}
	return false
}
func openFrontend() (*zip.Reader, error) {
	data, err := os.ReadFile(frontendRoot)
	if err != nil {
		return nil, err
	}
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}
func frontendDigest(source fs.FS, name string) (string, error) {
	data, err := fs.ReadFile(source, name)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
