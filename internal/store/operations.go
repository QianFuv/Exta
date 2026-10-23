package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

// Inspection describes verified local state without initializing or recovering it.
type Inspection struct {
	SchemaVersion     int    `json:"schema_version"`
	MigrationChecksum string `json:"migration_checksum"`
	Lineage           string `json:"lineage"`
	Generation        int64  `json:"activity_generation"`
	PageSize          int    `json:"page_size"`
	Unlocked          bool   `json:"unlocked"`
}

const resolvedResearch = `state IN ('succeeded','failed','cancelled') AND lease_token IS NULL AND EXISTS(SELECT 1 FROM budget_reservations b WHERE b.parent_id=research_jobs.id) AND NOT EXISTS(SELECT 1 FROM budget_reservations b WHERE b.parent_id=research_jobs.id AND (b.state='held' OR b.active_slot<>0))`

// AdvanceActivityTx makes rollback safety monotonic and rejects a damaged fence.
func AdvanceActivityTx(ctx context.Context, tx *sql.Tx) error {
	result, err := tx.ExecContext(ctx, "UPDATE activity_fence SET generation=generation+1 WHERE id=1 AND typeof(generation)='integer' AND generation>=0 AND generation<9223372036854775807")
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return Reject("storage_unavailable", 503, "Activity fence is unavailable or exhausted")
	}
	return nil
}

func databaseURI(path, mode string) string {
	path = filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		path = "/" + path
	}
	address := url.URL{Scheme: "file", Path: path}
	query := url.Values{"mode": {mode}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}
	address.RawQuery = query.Encode()
	return address.String()
}

func openInspection(path, mode string) (*sql.DB, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("database path must be absolute")
	}
	database, err := sql.Open("sqlite", databaseURI(path, mode))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	return database, nil
}

var expectedSchemas sync.Map

type schemaExpectation struct {
	Objects map[string]string
	Error   error
}

func schemaObjects(ctx context.Context, database *sql.DB) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, "SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT GLOB 'sqlite_*'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var kind, name, table, definition string
		if err = rows.Scan(&kind, &name, &table, &definition); err != nil {
			return nil, err
		}
		result[kind+":"+name+":"+table] = strings.ReplaceAll(strings.ReplaceAll(definition, "\r\n", "\n"), "\r", "\n")
	}
	return result, rows.Err()
}

func validateSchemaObjects(ctx context.Context, database *sql.DB, version int) error {
	cached, exists := expectedSchemas.Load(version)
	if !exists {
		expected := schemaExpectation{}
		build := func() {
			reference, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				expected.Error = err
				return
			}
			defer reference.Close()
			reference.SetMaxOpenConns(1)
			if _, err = reference.Exec(initialSchema); err != nil {
				expected.Error = err
				return
			}
			if version == 2 {
				if _, err = reference.Exec(keySchema); err != nil {
					expected.Error = err
					return
				}
			}
			expected.Objects, expected.Error = schemaObjects(context.Background(), reference)
		}
		build()
		cached, _ = expectedSchemas.LoadOrStore(version, expected)
	}
	expected := cached.(schemaExpectation)
	if expected.Error != nil {
		return expected.Error
	}
	actual, err := schemaObjects(ctx, database)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected.Objects, actual) {
		return errors.New("database schema objects differ from the published migration")
	}
	return nil
}

// InspectExisting reads WAL-aware state with mode=ro, never schema initialization.
func InspectExisting(ctx context.Context, path string, vault Vault) (Inspection, error) {
	var result Inspection
	database, err := openInspection(path, "ro")
	if err != nil {
		return result, err
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return result, err
	}
	count := 0
	for rows.Next() {
		var status string
		if err = rows.Scan(&status); err != nil {
			rows.Close()
			return result, err
		}
		if status != "ok" {
			rows.Close()
			return result, errors.New("database integrity check failed")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if count != 1 {
		return result, errors.New("database integrity result is incomplete")
	}
	rows, err = database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return result, err
	}
	hasViolation := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if hasViolation {
		return result, errors.New("database foreign key check failed")
	}
	if err = database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.SchemaVersion); err != nil {
		return result, err
	}
	if result.SchemaVersion != 1 && result.SchemaVersion != 2 {
		return result, errors.New("database schema is incompatible; initialization and downgrades are refused")
	}
	if err = validateSchemaObjects(ctx, database, result.SchemaVersion); err != nil {
		return result, err
	}
	if err = database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != result.SchemaVersion {
		return result, errors.New("migration history is incomplete or incompatible")
	}
	if err = database.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=1").Scan(&result.MigrationChecksum); err != nil {
		return result, err
	}
	if result.MigrationChecksum != Digest([]byte(strings.ReplaceAll(strings.ReplaceAll(initialSchema, "\r\n", "\n"), "\r", "\n"))) {
		return result, errors.New("migration checksum mismatch")
	}
	if result.SchemaVersion == 2 {
		var second string
		if err = database.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=2").Scan(&second); err != nil || second != schemaDigest(keySchema) {
			return result, errors.New("key migration checksum mismatch")
		}
		result.MigrationChecksum = Digest([]byte(result.MigrationChecksum + "\n" + second + "\n"))
	}
	if err = database.QueryRowContext(ctx, "SELECT count(*) FROM principals WHERE credential_hash IS NOT NULL").Scan(&count); err != nil || count == 0 {
		return result, errors.New("administrator initialization is incomplete")
	}
	if err = database.QueryRowContext(ctx, "SELECT generation FROM activity_fence WHERE id=1 AND typeof(generation)='integer' AND generation>=0 AND generation<9223372036854775807").Scan(&result.Generation); err != nil {
		return result, errors.New("activity fence is missing, invalid or exhausted")
	}
	if err = database.QueryRowContext(ctx, "PRAGMA page_size").Scan(&result.PageSize); err != nil {
		return result, err
	}
	var sentinel []byte
	if err = database.QueryRowContext(ctx, "SELECT ciphertext FROM secret_metadata WHERE id=1").Scan(&sentinel); err != nil || len(sentinel) == 0 {
		return result, errors.New("key verification sentinel is missing")
	}
	result.Lineage = Digest(sentinel)
	if vault == nil {
		return result, nil
	}
	plain, err := vault.Open("master-key-check", sentinel)
	if err != nil || string(plain) != "Exta master key verification v1" {
		return result, nil
	}
	for _, query := range []string{"SELECT id,ciphertext FROM credential_versions", "SELECT id || '/input',input_cipher FROM research_jobs WHERE NOT (coalesce(json_extract(data,'$.content_purged'),0)=1 AND length(input_cipher)=0 AND result_cipher IS NULL AND " + resolvedResearch + ")", "SELECT id || '/result',result_cipher FROM research_jobs WHERE result_cipher IS NOT NULL"} {
		rows, err = database.QueryContext(ctx, query)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var id string
			var cipher []byte
			if err = rows.Scan(&id, &cipher); err != nil {
				rows.Close()
				return result, err
			}
			if _, err = vault.Open(id, cipher); err != nil {
				rows.Close()
				return result, errors.New("retained encrypted state cannot be authenticated")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	result.Unlocked = true
	return result, nil
}

// Snapshot includes committed WAL state in an exclusively created SQLite snapshot.
// The caller must hold the source data-directory lock and protect the destination.
func Snapshot(ctx context.Context, source, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("snapshot destination must be absolute")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	database, err := openInspection(source, "ro")
	if err != nil {
		return err
	}
	defer database.Close()
	if _, err = database.ExecContext(ctx, "VACUUM main INTO ?", destination); err != nil {
		return err
	}
	file, err = os.OpenFile(destination, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

// RestoreSnapshot uses SQLite's backup transaction rather than replacing a WAL file.
// The caller verifies lineage, schema, activity fence and permissions under its lock.
func RestoreSnapshot(ctx context.Context, destination, source string) (resultErr error) {
	database, err := openInspection(destination, "rw")
	if err != nil {
		return err
	}
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return connection.Raw(func(raw any) (resultErr error) {
		restorer, ok := raw.(interface {
			NewRestore(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite restoration capability is unavailable")
		}
		backup, err := restorer.NewRestore(databaseURI(source, "ro"))
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, backup.Finish()) }()
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(128)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	})
}
