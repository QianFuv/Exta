package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"exta/internal/security"
	"exta/internal/store"
)

func hashReader(reader io.Reader) (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

type restoreMarker struct {
	Status            string `json:"status"`
	BackupId          string `json:"backup_id"`
	ManifestHash      string `json:"manifest_hash"`
	Lineage           string `json:"lineage"`
	Generation        int64  `json:"activity_generation"`
	RecoveryDirectory string `json:"recovery_directory"`
}

func checkRestoreComplete(directory string) error {
	path := filepath.Join(directory, restoreMarkerName)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var marker restoreMarker
	if _, err := decodeFile(path, 65536, &marker); err != nil {
		return err
	}
	if marker.Status != "complete" {
		return errors.New("an interrupted restore requires explicit recovery with the original backup")
	}
	return nil
}

func writeAtomic(path string, data []byte, beforePublish func() error) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".operation-staged-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err = privateObject(name, false); err != nil {
		return err
	}
	if beforePublish != nil {
		if err = beforePublish(); err != nil {
			return err
		}
	}
	return security.ReplaceFile(name, path)
}

func durableSnapshot(ctx context.Context, source, destination string) error {
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("snapshot destination must not exist")
	}
	staged := destination + ".pending-" + store.ID("snapshot")
	if err := store.Snapshot(ctx, source, staged); err != nil {
		return err
	}
	return security.ReplaceFile(staged, destination)
}

func restoreCommand(ctx context.Context, directory, source string, output io.Writer, checkpoint func(string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := disjointPaths(directory, source); err != nil {
		return err
	}
	lock, err := lockOperations(directory, true)
	if err != nil {
		return err
	}
	defer lock.Close()
	local, err := inspectLocal(ctx, directory)
	if err != nil {
		return err
	}
	if !local.State.Unlocked {
		return errors.New("restore requires the existing matching master key")
	}
	manifest, configuration, err := verifyBackup(ctx, source, local.Key)
	if err != nil {
		return err
	}
	if local.State.Lineage != manifest.State.Lineage || local.State.Generation != manifest.State.Generation || local.State.MigrationChecksum != manifest.State.MigrationChecksum || local.State.PageSize != manifest.State.PageSize {
		return errors.New("restore refused: destination identity, schema or activity differs; stale rollback may lose accepted work")
	}
	manifestPayload, err := payloadInfo(filepath.Join(source, "manifest.json"))
	if err != nil {
		return err
	}
	markerPath := filepath.Join(directory, restoreMarkerName)
	check := func(stage string) error {
		if checkpoint != nil {
			return checkpoint(stage)
		}
		return nil
	}
	marker := restoreMarker{Status: "pending", BackupId: manifest.Id, ManifestHash: manifestPayload.SHA256, Lineage: manifest.State.Lineage, Generation: manifest.State.Generation}
	var pending restoreMarker
	_, markerErr := os.Lstat(markerPath)
	if markerErr == nil {
		if _, err = decodeFile(markerPath, 65536, &pending); err != nil {
			return err
		}
	} else if !errors.Is(markerErr, os.ErrNotExist) {
		return markerErr
	}
	if errors.Is(markerErr, os.ErrNotExist) || pending.Status == "complete" {
		marker.RecoveryDirectory = filepath.Join(directory, store.ID("restore-recovery"))
		if err = newPrivateDirectory(marker.RecoveryDirectory); err != nil {
			return err
		}
		if err = durableSnapshot(ctx, filepath.Join(directory, "exta.sqlite"), filepath.Join(marker.RecoveryDirectory, "exta.sqlite")); err != nil {
			return err
		}
		if err = writeAtomic(filepath.Join(marker.RecoveryDirectory, "config.json"), local.ConfigBytes, nil); err != nil {
			return err
		}
		if err = security.SyncDirectory(marker.RecoveryDirectory); err != nil {
			return err
		}
		if err = security.SyncDirectory(directory); err != nil {
			return err
		}
		data, marshalErr := json.Marshal(marker)
		if marshalErr != nil {
			return marshalErr
		}
		if err = writeAtomic(markerPath, data, func() error { return check("marker_staged") }); err != nil {
			return err
		}
	} else {
		if pending.Status != "pending" || pending.BackupId != marker.BackupId || pending.ManifestHash != marker.ManifestHash || pending.Lineage != marker.Lineage || pending.Generation != marker.Generation {
			return errors.New("pending restore belongs to a different verified backup")
		}
		marker = pending
	}
	if err = check("before_database"); err != nil {
		return err
	}
	if err = store.RestoreSnapshot(ctx, filepath.Join(directory, "exta.sqlite"), filepath.Join(source, "exta.sqlite")); err != nil {
		return err
	}
	if err = check("after_database"); err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(directory, "config.json"), configuration, nil); err != nil {
		return err
	}
	if err = check("after_config"); err != nil {
		return err
	}
	restored, err := inspectLocal(ctx, directory)
	if err != nil {
		return err
	}
	if restored.State != manifest.State || store.Digest(restored.ConfigBytes) != manifest.Payloads["config.json"].SHA256 {
		return errors.New("restored state failed logical verification; pending marker retained")
	}
	if err = check("before_completion"); err != nil {
		return err
	}
	marker.Status = "complete"
	completion, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err = writeAtomic(markerPath, completion, nil); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Restored verified backup %s. Previous local recovery material retained at %s.\n", manifest.Id, marker.RecoveryDirectory)
	return err
}
