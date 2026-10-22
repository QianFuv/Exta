package main

import (
	"bytes"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exta/internal/security"
	"exta/internal/store"
)

func operationFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "runtime")
	if err := initialize(t.Context(), directory, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	return directory, filepath.Join(root, "backup ' 测试")
}

func TestBackupRestoreAndCurrentMigration(t *testing.T) {
	directory, backup := operationFixture(t)
	var output bytes.Buffer
	if err := run(t.Context(), []string{"backup", "--data-dir", directory, "--output", backup}, &output, &output); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(directory, "master.key"))
	manifest, originalConfig, err := verifyBackup(t.Context(), backup, key)
	if err != nil || manifest.State.Generation != 0 {
		t.Fatalf("backup: %+v %v", manifest, err)
	}
	if _, err = os.Stat(filepath.Join(backup, "master.key")); !os.IsNotExist(err) {
		t.Fatal("key bundled with database")
	}
	if err = security.CheckPrivate(filepath.Join(backup+".key-recovery", "master.key")); err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"listen":"127.0.0.1:8790","origin":"http://127.0.0.1:8790"}`)
	if err = os.WriteFile(filepath.Join(directory, "config.json"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err = run(t.Context(), []string{"restore", "--data-dir", directory, "--from", backup}, &output, &output); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(filepath.Join(directory, "config.json"))
	if !bytes.Equal(restored, originalConfig) {
		t.Fatal("config not restored exactly")
	}
	if err = run(t.Context(), []string{"backup", "--data-dir", directory, "--output", backup}, &output, &output); err == nil {
		t.Fatal("overwrote backup")
	}
	migration := filepath.Join(filepath.Dir(directory), "migration")
	output.Reset()
	if err = run(t.Context(), []string{"migrate", "--data-dir", directory, "--backup", migration}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "no migration applied") {
		t.Fatal("claimed a schema upgrade")
	}
}

func TestRestoreFailsClosedOnActivityIdentityAndCorruption(t *testing.T) {
	for _, kind := range []string{"activity", "lineage", "wrong_key", "payload", "manifest", "new_schema", "missing_fence", "sidecar"} {
		t.Run(kind, func(t *testing.T) {
			directory, backup := operationFixture(t)
			if err := backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "activity", "new_schema", "missing_fence":
				state, _, _, lock, err := openRuntime(t.Context(), directory)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "activity" {
					err = state.Write(t.Context(), func(tx *sql.Tx) error { return store.AdvanceActivityTx(t.Context(), tx) })
				} else if kind == "new_schema" {
					_, err = state.DB.Exec("PRAGMA user_version=2")
				} else {
					_, err = state.DB.Exec("DELETE FROM activity_fence")
				}
				state.Close()
				lock.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "lineage":
				other := filepath.Join(filepath.Dir(directory), "other")
				if err := initialize(t.Context(), other, new(bytes.Buffer)); err != nil {
					t.Fatal(err)
				}
				key, _ := os.ReadFile(filepath.Join(directory, "master.key"))
				vault, _ := security.NewVault(key)
				cipher, _ := vault.Seal("master-key-check", []byte("Exta master key verification v1"))
				state, _, _, lock, err := openRuntime(t.Context(), directory)
				if err != nil {
					t.Fatal(err)
				}
				_, err = state.DB.Exec("UPDATE secret_metadata SET ciphertext=? WHERE id=1", cipher)
				state.Close()
				lock.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "wrong_key":
				if err := os.WriteFile(filepath.Join(directory, "master.key"), bytes.Repeat([]byte{9}, 32), 0600); err != nil {
					t.Fatal(err)
				}
			case "payload":
				if err := os.WriteFile(filepath.Join(backup, "config.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(filepath.Join(backup, "manifest.sha256"), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "sidecar":
				if err := os.WriteFile(filepath.Join(backup, "exta.sqlite-wal"), []byte("unmanifested"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
			if err := restoreCommand(t.Context(), directory, backup, new(bytes.Buffer), nil); err == nil {
				t.Fatal("unsafe restore accepted")
			}
			after, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
			if before != after {
				t.Fatal("failed preflight mutated destination database")
			}
			if _, err := os.Stat(filepath.Join(directory, restoreMarkerName)); !os.IsNotExist(err) {
				t.Fatal("preflight failure left mutation marker")
			}
		})
	}
}

func TestInterruptedRestoreBlocksStartupAndResumesSameBackup(t *testing.T) {
	for _, stage := range []string{"before_database", "after_database", "after_config", "before_completion"} {
		t.Run(stage, func(t *testing.T) {
			directory, backup := operationFixture(t)
			if err := backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err != nil {
				t.Fatal(err)
			}
			interrupted := errors.New("synthetic interruption")
			err := restoreCommand(t.Context(), directory, backup, new(bytes.Buffer), func(at string) error {
				if at == stage {
					return interrupted
				}
				return nil
			})
			if !errors.Is(err, interrupted) {
				t.Fatalf("checkpoint: %v", err)
			}
			if _, _, _, _, err = openRuntime(t.Context(), directory); err == nil {
				t.Fatal("startup ignored interrupted restore")
			}
			if err = backupCommand(t.Context(), directory, backup+"-new", "", new(bytes.Buffer), false); err == nil {
				t.Fatal("backup ignored interrupted restore")
			}
			if err = restoreCommand(t.Context(), directory, backup, new(bytes.Buffer), nil); err != nil {
				t.Fatal(err)
			}
			state, _, _, lock, err := openRuntime(t.Context(), directory)
			if err != nil {
				t.Fatal(err)
			}
			state.Close()
			lock.Close()
		})
	}
}

func TestOperationsRequireStoppedServiceAndSeparateNewOutputs(t *testing.T) {
	directory, backup := operationFixture(t)
	state, _, _, lock, err := openRuntime(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if err = backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err == nil {
		t.Fatal("backed up while serving")
	}
	state.Close()
	lock.Close()
	for _, output := range []string{directory, filepath.Join(directory, "nested"), filepath.Dir(directory), "relative"} {
		if err = backupCommand(t.Context(), directory, output, "", new(bytes.Buffer), false); err == nil {
			t.Fatalf("unsafe output %s", output)
		}
	}
	if err = os.Remove(filepath.Join(directory, "master.key")); err != nil {
		t.Fatal(err)
	}
	if err = backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err == nil {
		t.Fatal("reported locked backup recoverable")
	}
	if _, err = os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("locked backup created output")
	}
}

func TestDoctorIsLocalAndReportsPortAndLockedKey(t *testing.T) {
	directory, _ := operationFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	configuration := config{Listen: listener.Addr().String(), Origin: "http://" + listener.Addr().String()}
	data, _ := json.Marshal(configuration)
	if err = os.WriteFile(filepath.Join(directory, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
	var output bytes.Buffer
	if err = doctor(t.Context(), directory, &output); err == nil || !strings.Contains(output.String(), `"name":"port","status":"failed"`) {
		t.Fatalf("port result: %s %v", output.String(), err)
	}
	listener.Close()
	if err = os.Remove(filepath.Join(directory, "master.key")); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err = doctor(t.Context(), directory, &output); err == nil || !strings.Contains(output.String(), `"name":"master_key","status":"failed"`) {
		t.Fatalf("key result: %s %v", output.String(), err)
	}
	after, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
	if before != after {
		t.Fatal("doctor mutated database")
	}
}

func TestRestorePublicationIsAtomicAndStartupReadsAfterLock(t *testing.T) {
	directory, backup := operationFixture(t)
	if err := backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("stop before marker publication")
	before, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
	if err := restoreCommand(t.Context(), directory, backup, new(bytes.Buffer), func(stage string) error {
		if stage == "marker_staged" {
			return interrupted
		}
		return nil
	}); !errors.Is(err, interrupted) {
		t.Fatalf("staging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, restoreMarkerName)); !os.IsNotExist(err) {
		t.Fatal("partially published marker visible")
	}
	after, _ := payloadInfo(filepath.Join(directory, "exta.sqlite"))
	if before != after {
		t.Fatal("database changed before marker was published")
	}
	lock, err := lockOperations(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "config.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = openRuntime(t.Context(), directory)
	if err == nil || !strings.Contains(err.Error(), "exclusive") {
		t.Fatalf("startup read configuration before acquiring lock: %v", err)
	}
	lock.Close()
}

func TestBackupTlsDependenciesAreCheckedBeforeRestoreMutation(t *testing.T) {
	directory, backup := operationFixture(t)
	server := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := server.TLS.Certificates[0]
	server.Close()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(directory, "tls.pem"), filepath.Join(directory, "tls.key")
	for path, data := range map[string][]byte{certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})} {
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(directory, "config.json")
	original, _ := os.ReadFile(configPath)
	data, _ := json.Marshal(config{Listen: "127.0.0.1:8789", Origin: "https://127.0.0.1:8789", TlsCertFile: certPath, TlsKeyFile: keyPath})
	if err = os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = backupCommand(t.Context(), directory, backup, "", new(bytes.Buffer), false); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(certPath); err != nil {
		t.Fatal(err)
	}
	if err = restoreCommand(t.Context(), directory, backup, new(bytes.Buffer), nil); err == nil {
		t.Fatal("missing TLS dependency accepted")
	}
	if _, err = os.Stat(filepath.Join(directory, restoreMarkerName)); !os.IsNotExist(err) {
		t.Fatal("mutated destination before validating backup TLS")
	}
	current, _ := os.ReadFile(configPath)
	if !bytes.Equal(current, original) {
		t.Fatal("installed unusable TLS configuration")
	}
}

func TestOperationPathsRejectParentTraversal(t *testing.T) {
	root := t.TempDir()
	path := root + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "target"
	if err := security.CheckNoLinks(path); err == nil {
		t.Fatal("parent traversal normalized before validation")
	}
	directory := filepath.Join(root, "private")
	if err := security.CreatePrivateDirectory(directory + string(filepath.Separator)); err != nil {
		t.Fatal(err)
	}
	if err := security.CheckPrivate(directory); err != nil {
		t.Fatal(err)
	}
}

func TestBackupDirectoryTrailingSeparators(t *testing.T) {
	for _, explicitKey := range []bool{false, true} {
		t.Run(store.JSON(explicitKey), func(t *testing.T) {
			directory, backup := operationFixture(t)
			keyOutput := ""
			if explicitKey {
				keyOutput = backup + "-separate-key" + string(filepath.Separator)
			}
			if err := backupCommand(t.Context(), directory, backup+string(filepath.Separator), keyOutput, new(bytes.Buffer), false); err != nil {
				t.Fatal(err)
			}
			key, _ := os.ReadFile(filepath.Join(directory, "master.key"))
			manifest, _, err := verifyBackup(t.Context(), backup, key)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.KeyRecoveryDirectory != filepath.Clean(manifest.KeyRecoveryDirectory) {
				t.Fatal("key-recovery identity was not normalized")
			}
		})
	}
}
