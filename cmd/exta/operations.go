package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
	"exta/internal/webassets"
)

const restoreMarkerName = "restore-operation.json"

type localInspection struct {
	State       store.Inspection
	Config      config
	ConfigBytes []byte
	Key         []byte
	Vault       *security.Vault
}

type backupPayload struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type backupManifest struct {
	Format               int                      `json:"format"`
	Id                   string                   `json:"id"`
	CreatedAt            string                   `json:"created_at"`
	Build                domain.BuildInfo         `json:"build"`
	State                store.Inspection         `json:"state"`
	KeyFingerprint       string                   `json:"key_fingerprint"`
	KeyRecoveryDirectory string                   `json:"key_recovery_directory"`
	EncryptionFormat     string                   `json:"encryption_format"`
	ExternalTlsFiles     []string                 `json:"external_tls_files"`
	Payloads             map[string]backupPayload `json:"payloads"`
}

func privateObject(path string, directory bool) error {
	if err := security.CheckNoLinks(path); err != nil {
		return err
	}
	if err := security.CheckPrivate(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return errors.New("state path has an unexpected file type")
	}
	return nil
}

func inspectLocal(ctx context.Context, directory string) (localInspection, error) {
	var result localInspection
	if err := privateObject(directory, true); err != nil {
		return result, err
	}
	for _, name := range []string{"config.json", "exta.sqlite", "master.key", "exta.lock", "exta.sqlite-wal", "exta.sqlite-shm"} {
		if err := privateObject(filepath.Join(directory, name), false); err != nil {
			if errors.Is(err, os.ErrNotExist) && name != "config.json" && name != "exta.sqlite" {
				continue
			}
			return result, err
		}
	}
	configuration, err := loadConfig(directory)
	if err != nil {
		return result, err
	}
	if _, err = security.NewAuth(nil, configuration.Origin, configuration.DevelopmentOrigins); err != nil {
		return result, err
	}
	if err = validateTlsFiles(configuration); err != nil {
		return result, err
	}
	result.Config = configuration
	result.ConfigBytes, err = os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		return result, err
	}
	result.Key, err = os.ReadFile(filepath.Join(directory, "master.key"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	result.Vault, err = security.NewVault(result.Key)
	if err != nil {
		result.Vault, _ = security.NewVault(nil)
	}
	result.State, err = store.InspectExisting(ctx, filepath.Join(directory, "exta.sqlite"), result.Vault)
	return result, err
}

func validateTlsFiles(configuration config) error {
	if configuration.TlsCertFile != "" {
		for _, path := range []string{configuration.TlsCertFile, configuration.TlsKeyFile} {
			if err := security.CheckNoLinks(path); err != nil {
				return errors.New("TLS dependencies require absolute paths without links")
			}
		}
		if err := privateObject(configuration.TlsKeyFile, false); err != nil {
			return errors.New("TLS private key permissions are unsafe")
		}
		if _, err := tls.LoadX509KeyPair(configuration.TlsCertFile, configuration.TlsKeyFile); err != nil {
			return errors.New("TLS certificate/key pair cannot be loaded")
		}
	}
	return nil
}

func lockOperations(directory string, allowPending bool) (*os.File, error) {
	if err := privateObject(directory, true); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "exta.lock")
	if err := privateObject(path, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock, err := security.LockDirectory(path)
	if err != nil {
		return nil, errors.New("operation requires a stopped service and exclusive data-directory lock")
	}
	if !allowPending {
		if err = checkRestoreComplete(directory); err != nil {
			lock.Close()
			return nil, errors.New("an interrupted restore must be completed first")
		}
	}
	return lock, nil
}

func doctor(ctx context.Context, directory string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	type check struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	checks := []check{}
	hasFailure := false
	add := func(name, detail string, err error) {
		status := "ok"
		if err != nil {
			status, detail, hasFailure = "failed", err.Error(), true
		}
		checks = append(checks, check{name, status, detail})
	}
	lock, err := lockOperations(directory, false)
	add("exclusive_state", "stopped service; local inspection only", err)
	if err == nil {
		defer lock.Close()
		local, inspectionErr := inspectLocal(ctx, directory)
		add("permissions_config_schema_integrity", "schema and all retained ciphertext inspected without initialization or recovery", inspectionErr)
		if inspectionErr == nil {
			var keyErr error
			if !local.State.Unlocked {
				keyErr = errors.New("master key is absent, invalid or cannot decrypt this database")
			}
			add("master_key", "verified existing key sentinel and retained ciphertext", keyErr)
			listener, portErr := net.Listen("tcp", local.Config.Listen)
			if portErr == nil {
				listener.Close()
			}
			add("port", "available at the instant of this local bind; not reserved", portErr)
		}
	}
	bundle, assetErr := webassets.Embedded()
	if assetErr == nil && !bundle.Available() {
		checks = append(checks, check{"assets", "deferred", "backend-only build"})
	} else {
		add("assets", "embedded production bundle integrity verified; browser acceptance recorded separately", assetErr)
	}
	if err := json.NewEncoder(output).Encode(struct {
		Build  domain.BuildInfo `json:"build"`
		Checks []check          `json:"checks"`
	}{buildInfo(), checks}); err != nil {
		return err
	}
	if hasFailure {
		return errors.New("local doctor found failed checks")
	}
	return nil
}

func disjointPaths(paths ...string) error {
	for index, path := range paths {
		if !filepath.IsAbs(path) {
			return errors.New("all operation paths must be absolute")
		}
		if err := security.CheckNoLinks(path); err != nil {
			return err
		}
		for _, other := range paths[:index] {
			for _, pair := range [][2]string{{path, other}, {other, path}} {
				relative, err := filepath.Rel(pair[0], pair[1])
				if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
					return errors.New("data, backup and key-recovery directories must be distinct and non-nested")
				}
			}
		}
	}
	return nil
}

func newPrivateDirectory(path string) error {
	if err := security.CheckNoLinks(path); err != nil {
		return err
	}
	if err := security.CreatePrivateDirectory(path); err != nil {
		return errors.New("output directory must be new, with an existing parent")
	}
	return privateObject(path, true)
}

func payloadInfo(path string) (backupPayload, error) {
	file, err := os.Open(path)
	if err != nil {
		return backupPayload{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return backupPayload{}, err
	}
	digest, err := hashReader(file)
	return backupPayload{info.Size(), digest}, err
}

func decodeFile(path string, limit int64, value any) ([]byte, error) {
	if err := privateObject(path, false); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("operation metadata exceeds its bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return nil, errors.New("invalid operation metadata")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("operation metadata must contain one JSON value")
	}
	return data, nil
}

func createBackup(ctx context.Context, directory, destination, keyDestination string, local localInspection) (backupManifest, error) {
	var manifest backupManifest
	if !local.State.Unlocked || len(local.Key) != 32 {
		return manifest, errors.New("a verified master key is required for a recoverable backup")
	}
	if err := security.CheckNoLinks(destination); err != nil {
		return manifest, err
	}
	destination = filepath.Clean(destination)
	if keyDestination == "" {
		keyDestination = destination + ".key-recovery"
	}
	if err := disjointPaths(directory, destination, keyDestination); err != nil {
		return manifest, err
	}
	directory, keyDestination = filepath.Clean(directory), filepath.Clean(keyDestination)
	for _, path := range []string{destination, keyDestination} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return manifest, errors.New("backup and key recovery outputs must not exist")
		}
	}
	if err := newPrivateDirectory(destination); err != nil {
		return manifest, err
	}
	if err := newPrivateDirectory(keyDestination); err != nil {
		return manifest, err
	}
	if err := writeAtomic(filepath.Join(keyDestination, "master.key"), local.Key, nil); err != nil {
		return manifest, err
	}
	if err := privateObject(filepath.Join(keyDestination, "master.key"), false); err != nil {
		return manifest, err
	}
	if err := durableSnapshot(ctx, filepath.Join(directory, "exta.sqlite"), filepath.Join(destination, "exta.sqlite")); err != nil {
		return manifest, err
	}
	if err := writeAtomic(filepath.Join(destination, "config.json"), local.ConfigBytes, nil); err != nil {
		return manifest, err
	}
	state, err := store.InspectExisting(ctx, filepath.Join(destination, "exta.sqlite"), local.Vault)
	if err != nil {
		return manifest, err
	}
	if state != local.State {
		return manifest, errors.New("snapshot identity changed during backup")
	}
	manifest = backupManifest{Format: 1, Id: store.ID("backup"), CreatedAt: store.Stamp(time.Now()), Build: buildInfo(), State: state, KeyFingerprint: store.Digest(local.Key), KeyRecoveryDirectory: keyDestination, EncryptionFormat: "Exta/v1/AES-256-GCM", ExternalTlsFiles: []string{}, Payloads: map[string]backupPayload{}}
	if local.Config.TlsCertFile != "" {
		manifest.ExternalTlsFiles = []string{local.Config.TlsCertFile, local.Config.TlsKeyFile}
	}
	for _, name := range []string{"exta.sqlite", "config.json"} {
		payload, err := payloadInfo(filepath.Join(destination, name))
		if err != nil {
			return manifest, err
		}
		manifest.Payloads[name] = payload
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest, err
	}
	data = append(data, '\n')
	if err = writeAtomic(filepath.Join(destination, "manifest.sha256"), []byte(store.Digest(data)+"\n"), nil); err != nil {
		return manifest, err
	}
	for _, path := range []string{keyDestination, filepath.Dir(keyDestination), destination, filepath.Dir(destination)} {
		if err = security.SyncDirectory(path); err != nil {
			return manifest, err
		}
	}
	if err = writeAtomic(filepath.Join(destination, "manifest.json"), data, nil); err != nil {
		return manifest, err
	}
	_, _, err = verifyBackup(ctx, destination, local.Key)
	return manifest, err
}

func verifyBackup(ctx context.Context, directory string, key []byte) (backupManifest, []byte, error) {
	var manifest backupManifest
	if err := privateObject(directory, true); err != nil {
		return manifest, nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(filepath.Join(directory, "exta.sqlite"+suffix)); !errors.Is(err, os.ErrNotExist) {
			return manifest, nil, errors.New("backup must be a standalone snapshot without unmanifested SQLite sidecars")
		}
	}
	data, err := decodeFile(filepath.Join(directory, "manifest.json"), 65536, &manifest)
	if err != nil {
		return manifest, nil, err
	}
	if err = privateObject(filepath.Join(directory, "manifest.sha256"), false); err != nil {
		return manifest, nil, err
	}
	if info, err := os.Stat(filepath.Join(directory, "manifest.sha256")); err != nil || info.Size() != 65 {
		return manifest, nil, errors.New("invalid manifest checksum size")
	}
	hash, err := os.ReadFile(filepath.Join(directory, "manifest.sha256"))
	if err != nil || string(hash) != store.Digest(data)+"\n" {
		return manifest, nil, errors.New("backup manifest checksum mismatch")
	}
	if manifest.Format != 1 || manifest.Id == "" || manifest.EncryptionFormat != "Exta/v1/AES-256-GCM" || len(key) != 32 || manifest.KeyFingerprint != store.Digest(key) || !manifest.State.Unlocked || len(manifest.Payloads) != 2 {
		return manifest, nil, errors.New("backup identity, key or format is incompatible")
	}
	for _, name := range []string{"exta.sqlite", "config.json"} {
		if err = privateObject(filepath.Join(directory, name), false); err != nil {
			return manifest, nil, err
		}
		actual, err := payloadInfo(filepath.Join(directory, name))
		if err != nil {
			return manifest, nil, err
		}
		if actual != manifest.Payloads[name] {
			return manifest, nil, errors.New("backup payload checksum or size mismatch")
		}
	}
	vault, _ := security.NewVault(key)
	state, err := store.InspectExisting(ctx, filepath.Join(directory, "exta.sqlite"), vault)
	if err != nil {
		return manifest, nil, err
	}
	if state != manifest.State {
		return manifest, nil, errors.New("backup schema, lineage or activity evidence mismatch")
	}
	configuration, err := loadConfig(directory)
	if err != nil {
		return manifest, nil, err
	}
	if _, err = security.NewAuth(nil, configuration.Origin, configuration.DevelopmentOrigins); err != nil {
		return manifest, nil, err
	}
	if err = validateTlsFiles(configuration); err != nil {
		return manifest, nil, err
	}
	configurationBytes, err := os.ReadFile(filepath.Join(directory, "config.json"))
	return manifest, configurationBytes, err
}

func backupCommand(ctx context.Context, directory, destination, keyDestination string, output io.Writer, migration bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	lock, err := lockOperations(directory, false)
	if err != nil {
		return err
	}
	defer lock.Close()
	local, err := inspectLocal(ctx, directory)
	if err != nil {
		return err
	}
	manifest, err := createBackup(ctx, directory, destination, keyDestination, local)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(output, "Verified backup %s at %s; separately protected key recovery: %s\n", manifest.Id, destination, manifest.KeyRecoveryDirectory); err != nil {
		return err
	}
	if migration {
		_, err = fmt.Fprintln(output, "Schema 1 is already current; no migration applied.")
	}
	return err
}
