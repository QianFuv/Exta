// Command release builds local artifacts and records evidence without publishing them.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"exta/internal/webassets"
)

type artifact struct {
	Target              string   `json:"target"`
	File                string   `json:"file"`
	Size                int64    `json:"size"`
	SHA256              string   `json:"sha256"`
	Archive             string   `json:"archive"`
	CompressedSize      int64    `json:"compressed_size"`
	ArchiveSHA256       string   `json:"archive_sha256"`
	Imports             []string `json:"system_imports"`
	RuntimeEvidence     string   `json:"runtime_evidence"`
	MinimalBaselineSize int64    `json:"minimal_baseline_size"`
	SizeAboveBaseline   int64    `json:"size_above_baseline"`
}

type module struct {
	Path, Version, Dir, Sum string
	Main                    bool
	Replace                 *module
}
type licenseEntry struct {
	Module  string            `json:"module"`
	Version string            `json:"version"`
	Sum     string            `json:"sum"`
	Files   map[string]string `json:"files"`
	Status  string            `json:"status"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command(ctx context.Context, environment []string, args ...string) ([]byte, error) {
	process := exec.CommandContext(ctx, args[0], args[1:]...)
	process.Env = environment
	output, err := process.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w\n%s", args[0], err, output)
	}
	return output, nil
}

func buildEnvironment(target string) []string {
	result := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOTOOLCHAIN", "GOFLAGS":
			continue
		}
		result = append(result, value)
	}
	parts := strings.Split(target, "/")
	return append(result, "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
}

func digestFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	return hex.EncodeToString(digest.Sum(nil)), size, err
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func sourceFiles() (map[string]string, error) {
	files := map[string]string{}
	for _, root := range []string{"cmd", "internal", "tests", "tools", "api", "web", "go.mod", "go.sum", ".node-version", ".npmrc"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if root == "web" && (entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "test-results" || entry.Name() == "playwright-report" || entry.Name() == "coverage") {
					return filepath.SkipDir
				}
				return nil
			}
			if root == "web" && strings.HasSuffix(path, ".tsbuildinfo") {
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return errors.New("release sources cannot be links")
			}
			hash, _, err := digestFile(path)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(path)] = hash
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func licenses(ctx context.Context, goBinary, destination string) ([]licenseEntry, error) {
	data, err := command(ctx, os.Environ(), goBinary, "list", "-mod=readonly", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	result := []licenseEntry{}
	for {
		var item module
		if err = decoder.Decode(&item); err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if item.Replace != nil {
			return nil, errors.New("release dependency replacement requires explicit review")
		}
		entry := licenseEntry{Module: item.Path, Version: item.Version, Sum: item.Sum, Files: map[string]string{}, Status: "not-declared"}
		if item.Dir == "" {
			entry.Status = "module-graph-only; source-not-cached"
			result = append(result, entry)
			continue
		}
		entries, err := os.ReadDir(item.Dir)
		if err != nil {
			return nil, err
		}
		for _, file := range entries {
			upper := strings.ToUpper(file.Name())
			if file.IsDir() || !(strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "LICENCE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE") || strings.HasPrefix(upper, "COPYRIGHT")) {
				continue
			}
			path := filepath.Join(item.Dir, file.Name())
			hash, _, err := digestFile(path)
			if err != nil {
				return nil, err
			}
			name := fmt.Sprintf("licenses/%03d-%s", len(result), file.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if err = os.WriteFile(filepath.Join(destination, filepath.FromSlash(name)), content, 0600); err != nil {
				return nil, err
			}
			entry.Files[name] = hash
			entry.Status = "files-collected"
		}
		result = append(result, entry)
	}
	goLicense, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "LICENSE"))
	if err != nil {
		return nil, err
	}
	goHash := sha256.Sum256(goLicense)
	goName := "licenses/go-runtime-LICENSE"
	if err = os.WriteFile(filepath.Join(destination, filepath.FromSlash(goName)), goLicense, 0600); err != nil {
		return nil, err
	}
	result = append(result, licenseEntry{Module: "go/runtime", Version: runtime.Version(), Files: map[string]string{goName: hex.EncodeToString(goHash[:])}, Status: "files-collected"})
	return result, nil
}

func binaryImports(path, target string) ([]string, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, err
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"]+"/"+settings["GOARCH"] != target || settings["CGO_ENABLED"] != "0" {
		return nil, errors.New("binary target/CGO settings differ from release contract")
	}
	var imports []string
	switch strings.Split(target, "/")[0] {
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		imports, err = peImports(file)
		if err != nil {
			return nil, err
		}
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		for _, program := range file.Progs {
			if program.Type == elf.PT_INTERP {
				return nil, errors.New("Linux candidate unexpectedly requires a dynamic loader")
			}
		}
		imports, err = file.ImportedLibraries()
		if err != nil {
			return nil, err
		}
		if len(imports) > 0 {
			return nil, errors.New("Linux candidate unexpectedly links shared libraries")
		}
	default:
		return nil, errors.New("unsupported release target")
	}
	for _, name := range imports {
		if strings.Contains(strings.ToLower(name), "sqlite") || strings.Contains(strings.ToLower(name), "node") {
			return nil, errors.New("unexpected runtime dependency")
		}
	}
	return imports, nil
}

func archive(destination, binary string) error {
	file, err := os.OpenFile(filepath.Join(destination, binary+".zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	paths := []string{binary, "RELEASE-NOTES.txt", "dependency-inventory.json"}
	entries, err := os.ReadDir(filepath.Join(destination, "licenses"))
	if err != nil {
		file.Close()
		return err
	}
	for _, entry := range entries {
		paths = append(paths, "licenses/"+entry.Name())
	}
	if _, err := os.Stat(filepath.Join(destination, "frontend-provenance.json")); err == nil {
		paths = append(paths, "frontend-provenance.json")
		err = filepath.WalkDir(filepath.Join(destination, "frontend-licenses"), func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(destination, name)
			if err == nil {
				paths = append(paths, filepath.ToSlash(relative))
			}
			return err
		})
		if err != nil {
			file.Close()
			return err
		}
	}
	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(path)))
		if err != nil {
			file.Close()
			return err
		}
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		header.SetMode(0644)
		if path == binary {
			header.SetMode(0755)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			file.Close()
			return err
		}
		if _, err = entry.Write(content); err != nil {
			file.Close()
			return err
		}
	}
	if err = writer.Close(); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "verify" {
		return errors.New("usage: release verify --output <new-directory> [--version value] [--backend-only]")
	}
	flags := flag.NewFlagSet("release verify", flag.ContinueOnError)
	output := flags.String("output", "", "New artifact directory")
	version := flags.String("version", "candidate", "Embedded application version")
	backendOnly := flags.Bool("backend-only", false, "Explicitly defer embedded UI acceptance")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *output == "" || !regexp.MustCompile(`^[A-Za-z0-9.+_-]{1,64}$`).MatchString(*version) {
		return errors.New("invalid release arguments")
	}
	files, err := sourceFiles()
	if err != nil {
		return err
	}
	frontend, err := openFrontend()
	if err != nil {
		return err
	}
	bundleFiles, err := fs.Sub(frontend, "bundle")
	if err != nil {
		return err
	}
	bundle, err := webassets.Load(bundleFiles)
	if err != nil {
		return err
	}
	if !*backendOnly && !bundle.Available() {
		return errors.New("production release requires verified embedded UI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	toolchain, err := command(ctx, os.Environ(), goBinary, "version")
	if err != nil {
		return err
	}
	head, err := command(ctx, os.Environ(), "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	contractData, err := os.ReadFile("api/contract-manifest.json")
	if err != nil {
		return err
	}
	var contract struct {
		SHA256 string `json:"contract_sha256"`
	}
	if err = json.Unmarshal(contractData, &contract); err != nil || len(contract.SHA256) != 64 {
		return errors.New("invalid source contract manifest")
	}
	openAPI, err := os.ReadFile("api/openapi.yaml")
	if err != nil {
		return err
	}
	contractDigest := sha256.Sum256([]byte(strings.ReplaceAll(string(openAPI), "\r\n", "\n")))
	if hex.EncodeToString(contractDigest[:]) != contract.SHA256 {
		return errors.New("source contract hash differs from manifest")
	}
	if bundle.Available() {
		if err := verifyFrontend(contract.SHA256); err != nil {
			return err
		}
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return errors.New("release output must be a new directory with an existing parent")
	}
	if err = os.Mkdir(filepath.Join(destination, "licenses"), 0700); err != nil {
		return err
	}
	inventory, err := licenses(ctx, goBinary, destination)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(destination, "dependency-inventory.json"), inventory); err != nil {
		return err
	}
	if bundle.Available() {
		if err := copyFrontendEvidence(destination); err != nil {
			return err
		}
	}
	notes := "Exta local candidate for Windows/Linux x64. No publication, signing or live provider validation. Native runtime and browser evidence must be recorded separately and tied to each binary hash; cross-build success is not runtime acceptance. Application license is not declared; this inventory makes no licensing grant. Dependency license texts are included from locked Go and shipped frontend packages.\n"
	if err = os.WriteFile(filepath.Join(destination, "RELEASE-NOTES.txt"), []byte(notes), 0600); err != nil {
		return err
	}
	artifacts := []artifact{}
	baselineDirectory := filepath.Join(destination, "baseline")
	if err = os.Mkdir(baselineDirectory, 0700); err != nil {
		return err
	}
	baselineSource := filepath.Join(baselineDirectory, "main.go")
	if err = os.WriteFile(baselineSource, []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		return err
	}
	buildFlags := []string{"build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-ldflags=-s -w -X main.version=" + *version}
	for _, target := range []string{"windows/amd64", "linux/amd64"} {
		fmt.Println("Building " + target)
		name := "exta-" + strings.ReplaceAll(target, "/", "-")
		if strings.HasPrefix(target, "windows/") {
			name += ".exe"
		}
		arguments := append([]string{goBinary}, buildFlags...)
		arguments = append(arguments, "-o", filepath.Join(destination, name), "./cmd/exta")
		if _, err = command(ctx, buildEnvironment(target), arguments...); err != nil {
			return err
		}
		baselineArguments := append([]string{goBinary}, buildFlags...)
		baselineArguments = append(baselineArguments, "-o", filepath.Join(baselineDirectory, name), baselineSource)
		if _, err = command(ctx, buildEnvironment(target), baselineArguments...); err != nil {
			return err
		}
		_, baselineSize, err := digestFile(filepath.Join(baselineDirectory, name))
		if err != nil {
			return err
		}
		imports, err := binaryImports(filepath.Join(destination, name), target)
		if err != nil {
			return err
		}
		info, err := buildinfo.ReadFile(filepath.Join(destination, name))
		if err != nil {
			return err
		}
		for _, dependency := range info.Deps {
			found := false
			for _, entry := range inventory {
				if entry.Module == dependency.Path && entry.Version == dependency.Version && entry.Status == "files-collected" {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("linked dependency license evidence missing: %s %s", dependency.Path, dependency.Version)
			}
		}
		hash, size, err := digestFile(filepath.Join(destination, name))
		if err != nil {
			return err
		}
		if err = archive(destination, name); err != nil {
			return err
		}
		archiveHash, compressedSize, err := digestFile(filepath.Join(destination, name+".zip"))
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact{Target: target, File: name, Size: size, SHA256: hash, Archive: name + ".zip", CompressedSize: compressedSize, ArchiveSHA256: archiveHash, Imports: imports, RuntimeEvidence: "build-only; see separately recorded smoke evidence", MinimalBaselineSize: baselineSize, SizeAboveBaseline: size - baselineSize})
	}
	after, err := sourceFiles()
	if err != nil {
		return err
	}
	beforeJSON, _ := json.Marshal(files)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		return errors.New("release inputs changed during builds; candidate not verified")
	}
	manifest := map[string]any{"format": 1, "created_at": time.Now().UTC().Format(time.RFC3339), "version": *version, "head": strings.TrimSpace(string(head)), "toolchain": strings.TrimSpace(string(toolchain)), "contract_sha256": contract.SHA256, "backend_only": *backendOnly, "flags": buildFlags, "environment": map[string]string{"CGO_ENABLED": "0", "GOTOOLCHAIN": "local"}, "source_files": files, "artifacts": artifacts}
	if err = writeJSON(filepath.Join(destination, "release-manifest.json"), manifest); err != nil {
		return err
	}
	checksums := ""
	for _, item := range artifacts {
		checksums += item.SHA256 + "  " + item.File + "\n" + item.ArchiveSHA256 + "  " + item.Archive + "\n"
	}
	for _, name := range []string{"release-manifest.json", "dependency-inventory.json", "RELEASE-NOTES.txt"} {
		hash, _, err := digestFile(filepath.Join(destination, name))
		if err != nil {
			return err
		}
		checksums += hash + "  " + name + "\n"
	}
	if err = os.WriteFile(filepath.Join(destination, "SHA256SUMS"), []byte(checksums), 0600); err != nil {
		return err
	}
	fmt.Println("Verified cross-build artifacts at " + destination)
	return nil
}
