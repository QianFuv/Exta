// Package store owns durable state and short atomic SQLite transactions.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"exta/internal/domain"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001.sql
var initialSchema string

// Vault keeps encryption keys outside the database and binds ciphertext to identity.
type Vault interface {
	Seal(entity string, plaintext []byte) ([]byte, error)
	Open(entity string, ciphertext []byte) ([]byte, error)
}

// Store serializes database access; network work must never hold a transaction.
type Store struct {
	DB    *sql.DB
	Vault Vault
	Path  string
	Now   func() time.Time
}

// Fault is a sanitized application failure safe for transport mapping.
type Fault struct {
	Code         string
	Status       int
	Message      string
	RetryAfterMs int64
	Details      map[string]any
}

func (fault *Fault) Error() string { return fault.Message }

// Reject creates a public failure without including underlying SQL or secrets.
func Reject(code string, status int, message string) *Fault {
	return &Fault{Code: code, Status: status, Message: message, Details: map[string]any{}}
}

// Open opens or initializes a database in an already-protected directory.
// CLI serve checks existence first; only explicit init and tests create new state.
func Open(ctx context.Context, path string, vault Vault) (*Store, error) {
	return openDatabase(ctx, path, vault, true)
}

// OpenExisting refuses uninitialized or partially bootstrapped state.
func OpenExisting(ctx context.Context, path string, vault Vault) (*Store, error) {
	return openDatabase(ctx, path, vault, false)
}

func openDatabase(ctx context.Context, path string, vault Vault, canInitialize bool) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	flags := os.O_RDWR
	if canInitialize {
		flags |= os.O_CREATE
	}
	file, err := os.OpenFile(absolute, flags, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if filepath.VolumeName(absolute) != "" {
		uriPath = "/" + uriPath
	}
	address := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	if !canInitialize {
		query.Set("mode", "rw")
	}
	address.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", address.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	state := &Store{DB: database, Vault: vault, Path: absolute, Now: time.Now}
	if err = state.initialize(ctx, canInitialize); err != nil {
		database.Close()
		return nil, err
	}
	return state, nil
}

func (state *Store) initialize(ctx context.Context, canInitialize bool) error {
	return state.Write(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		digest := Digest([]byte(strings.ReplaceAll(strings.ReplaceAll(initialSchema, "\r\n", "\n"), "\r", "\n")))
		if version == 0 {
			if !canInitialize {
				return Reject("storage_unavailable", 503, "Database initialization is incomplete")
			}
			if _, err := tx.ExecContext(ctx, initialSchema); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(1,?,?)", digest, Stamp(state.Now())); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
				return err
			}
			if err := seed(ctx, tx, state.Now()); err != nil {
				return err
			}
		} else if version != 1 {
			return Reject("storage_unavailable", 503, "Database schema is incompatible with this binary")
		}
		var stored string
		if err := tx.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=1").Scan(&stored); err != nil {
			return err
		}
		if stored != digest {
			return Reject("storage_unavailable", 503, "Database migration checksum mismatch")
		}
		if !canInitialize {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM principals WHERE credential_hash IS NOT NULL").Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				return Reject("storage_unavailable", 503, "Administrator initialization is incomplete")
			}
		}
		if state.Vault != nil {
			var ciphertext []byte
			err := tx.QueryRowContext(ctx, "SELECT ciphertext FROM secret_metadata WHERE id=1").Scan(&ciphertext)
			if errors.Is(err, sql.ErrNoRows) {
				ciphertext, err = state.Vault.Seal("master-key-check", []byte("Exta master key verification v1"))
				if err != nil {
					state.Vault = nil
					return nil
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO secret_metadata VALUES(1,?)", ciphertext)
				return err
			}
			if err != nil {
				return err
			}
			plaintext, err := state.Vault.Open("master-key-check", ciphertext)
			if err != nil || string(plaintext) != "Exta master key verification v1" {
				state.Vault = nil
			}
		}
		return nil
	})
}

// Write commits all changes atomically or rolls them all back.
func (state *Store) Write(ctx context.Context, change func(*sql.Tx) error) error {
	tx, err := state.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = change(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes database handles after callers stop admission and workers.
func (state *Store) Close() error { return state.DB.Close() }

// ID generates a local opaque identifier, never an upstream credential.
func ID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(value[:])
}

// Digest hashes opaque tokens or immutable source data without retaining clear values.
func Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// Stamp is the sortable fixed-width UTC storage timestamp.
func Stamp(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// JSON encodes trusted internal values; unsupported values are a programmer error.
func JSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// Decode reads a persisted contract value and reports corrupted storage safely.
func Decode[T any](data string) (T, error) {
	var value T
	err := json.Unmarshal([]byte(data), &value)
	if err != nil {
		return value, Reject("storage_unavailable", 503, "Stored record is unreadable")
	}
	return value, nil
}

// Missing maps absent rows without exposing SQL details.
func Missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return Reject("not_found", 404, "Resource not found")
	}
	return err
}

// AuditTx records bounded, sanitized mutation metadata in the same transaction.
func AuditTx(ctx context.Context, tx *sql.Tx, actor, action, entity string, changes any, now time.Time) error {
	event := domain.AuditEvent{Id: ID("audit"), ActorPrincipalId: actor, Action: action, EntityId: &entity, CreatedAt: Stamp(now), Changes: json.RawMessage(JSON(changes))}
	_, err := tx.ExecContext(ctx, "INSERT INTO audit_events VALUES(?,?,?,?,?,?)", event.Id, actor, action, entity, event.CreatedAt, JSON(event))
	return err
}

func seed(ctx context.Context, tx *sql.Tx, now time.Time) error {
	settings := domain.Settings{Version: 1, Limits: domain.Limits{RequestBytes: 1 << 20, ResponseBytes: 8 << 20, SynchronousConcurrency: 16, SynchronousWaiting: 64, PrincipalRequestsPerMinute: 60, ResearchQueued: 100, ResearchActive: 2, PrincipalResearchActive: 1, ObservationSeconds: 3600, MaxPolls: 120}, Retention: domain.Retention{RequestDays: 30, ResearchContentDays: 30, AuditDays: 90, IdempotencyDays: 90}, ProtectedFields: []string{"bind", "data_dir", "master_key", "upstream_origins"}}
	if _, err := tx.ExecContext(ctx, "INSERT INTO settings VALUES(1,1,?)", JSON(settings)); err != nil {
		return err
	}
	for _, operation := range []string{"search", "advanced_search", "fetch", "map", "crawl", "research_create"} {
		targets := []domain.RouteTarget{{Provider: "exa", Profile: "common"}, {Provider: "tavily", Profile: "common"}}
		deadline := domain.Integer(60)
		if operation == "search" {
			deadline = 30
		}
		if operation == "map" || operation == "crawl" {
			targets = targets[1:]
			deadline = 165
		}
		if operation == "research_create" {
			targets = []domain.RouteTarget{{Provider: "exa", Profile: "exa_auto"}, {Provider: "tavily", Profile: "tavily_mini"}}
			deadline = 30
		}
		policy := domain.RoutePolicy{Capability: domain.Operation(operation), Revision: 1, Enabled: true, Targets: targets, MaxAttempts: 3, DeadlineSeconds: deadline, CreatedAt: Stamp(now)}
		if _, err := tx.ExecContext(ctx, "INSERT INTO route_policies VALUES(?,1,?)", operation, JSON(policy)); err != nil {
			return fmt.Errorf("seed routes: %w", err)
		}
	}
	return nil
}
