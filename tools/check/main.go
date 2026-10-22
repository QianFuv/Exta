// Command check verifies source formatting, runtime dependency boundaries and
// generated contract consistency. It never installs or upgrades tools.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	for _, root := range []string{"cmd", "internal", "tools", "tests"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "dist" || entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
			formatted, err := format.Source(data)
			if err != nil {
				return fmt.Errorf("invalid Go source %s: %w", path, err)
			}
			if !bytes.Equal(data, formatted) {
				return fmt.Errorf("gofmt required: %s", path)
			}
			if (root == "cmd" || root == "internal") && !strings.HasSuffix(path, "_test.go") {
				file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, item := range file.Imports {
					imported, err := strconv.Unquote(item.Path.Value)
					if err != nil {
						return err
					}
					if imported == "C" || imported == "os/exec" {
						return fmt.Errorf("runtime must remain CGO-free without child processes: %s imports %s", path, imported)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	tracked := exec.Command("git", "ls-files", "-z")
	output, err := tracked.Output()
	if err != nil {
		return errors.New("cannot inspect tracked runtime-state exclusions")
	}
	for _, path := range strings.Split(string(output), "\x00") {
		name := filepath.Base(path)
		if strings.HasSuffix(path, ".sqlite") || name == "master.key" || name == ".env" || path == "config.json" {
			return fmt.Errorf("runtime state must remain external: %s", path)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "--backend-only" {
		return checkBackendArtifacts()
	}
	command := exec.Command("go", "run", "./tools/contract", "check")
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err = command.Run(); err != nil {
		return err
	}
	fmt.Println("Source formatting, runtime boundaries and contract consistency passed")
	return nil
}

func checkBackendArtifacts() error {
	data, err := os.ReadFile("api/contract-manifest.json")
	if err != nil {
		return err
	}
	var manifest struct {
		ContractSHA256 string            `json:"contract_sha256"`
		Files          map[string]string `json:"files"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	manifest.Files["api/openapi.yaml"] = manifest.ContractSHA256
	for path, expected := range manifest.Files {
		if strings.HasPrefix(path, "web/") {
			continue
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expected {
			return fmt.Errorf("backend contract artifact drift: %s", path)
		}
	}
	fmt.Println("Backend formatting, runtime boundaries and frozen artifact hashes passed; frontend checks intentionally excluded")
	return nil
}
