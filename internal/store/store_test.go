package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

const fakeCredential = "synthetic-operator-for-tests-only"
const fakeProviderKey = "synthetic-upstream-key-never-networked"

func openStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	vault, err := security.NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "exta.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	principal, err := state.BootstrapAdmin(t.Context(), fakeCredential)
	if err != nil {
		t.Fatal(err)
	}
	return state, principal
}

func addAccount(t *testing.T, state *store.Store, principal string, allocation int64) (domain.UpstreamKey, domain.UpstreamKey) {
	t.Helper()
	key, err := state.AddKey(t.Context(), principal, domain.UpstreamKeyCreate{Provider: "exa", QuotaPolicy: &domain.QuotaPolicy{Unit: "micro_usd", Allocation: domain.Integer(allocation), PerOperationReservation: 10, ActiveJobs: 2}, Label: "Test key", Enabled: true, Secret: fakeProviderKey})
	if err != nil {
		t.Fatal(err)
	}
	account := key
	return account, key
}

func faultCode(t *testing.T, err error, code string) {
	t.Helper()
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestMigrationsSeedsAndIncompatibleState(t *testing.T) {
	state, _ := openStore(t)
	routes, err := state.Routes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 6 {
		t.Fatalf("route count %d", len(routes))
	}
	for _, route := range routes {
		if route.Revision != 1 {
			t.Fatal("route not seeded")
		}
		if err = domain.ValidateJSON("RoutePolicy", []byte(store.JSON(route))); err != nil {
			t.Fatal(err)
		}
	}
	path := state.Path
	vault := state.Vault
	state.Close()
	reopened, err := store.Open(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.DB.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	_, err = store.Open(t.Context(), path, vault)
	faultCode(t, err, "storage_unavailable")
}

func TestMigrationChecksumFailsClosed(t *testing.T) {
	state, _ := openStore(t)
	path := state.Path
	vault := state.Vault
	if _, err := state.DB.Exec("UPDATE schema_migrations SET checksum='changed'"); err != nil {
		t.Fatal(err)
	}
	state.Close()
	_, err := store.Open(t.Context(), path, vault)
	faultCode(t, err, "storage_unavailable")
}

func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	state, principal := openStore(t)
	account, _ := addAccount(t, state, principal, 30)
	var succeeded atomic.Int32
	var workers sync.WaitGroup
	for index := 0; index < 30; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search", Amount: 10})
			if err == nil {
				succeeded.Add(1)
			} else {
				var fault *store.Fault
				if !errors.As(err, &fault) {
					t.Errorf("reserve: %v", err)
				}
			}
		}()
	}
	workers.Wait()
	if succeeded.Load() != 3 {
		t.Fatalf("admitted %d, budget permits 3", succeeded.Load())
	}
	if err := state.Write(t.Context(), func(tx *sql.Tx) error {
		used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if used != 0 || held != 30 {
			t.Errorf("used=%d held=%d", used, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownCostRetainsBudgetAndSettlementIsUnique(t *testing.T) {
	state, principal := openStore(t)
	account, _ := addAccount(t, state, principal, 100)
	hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("job"), PrincipalId: principal, Provider: "exa", Operation: "research_create", Amount: 50, Research: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.Settle(t.Context(), hold.Id, nil, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error {
		item, err := store.ReservationTx(t.Context(), tx, hold.Id)
		if item.ActiveSlot || item.State != "held" || item.CapacityBasis != "verified_terminal" {
			t.Errorf("unknown cost discarded hold: %+v", item)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cost := int64(23)
	for index := 0; index < 2; index++ {
		if err = state.Settle(t.Context(), hold.Id, &cost, "reported"); err != nil {
			t.Fatal(err)
		}
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error {
		used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if used != 23 || held != 0 {
			t.Errorf("duplicate settlement: %d/%d", used, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRestartPreservesHoldsCooldownAndPinnedCredential(t *testing.T) {
	state, principal := openStore(t)
	_, key := addAccount(t, state, principal, 100)
	hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("job"), PrincipalId: principal, Provider: "exa", Operation: "research_create", Amount: 50, Research: true})
	if err != nil {
		t.Fatal(err)
	}
	rotated := "synthetic-rotated-upstream-key-only"
	if _, err = state.ChangeKey(t.Context(), principal, key.Id, 1, nil, nil, &rotated, false); err != nil {
		t.Fatal(err)
	}
	if err = state.GateFailure(t.Context(), "key", key.Id, hold.CredentialVersionId, "invalid", 0); err != nil {
		t.Fatal(err)
	}
	keys, err := state.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].Validity == "invalid" || keys[0].RetainedCredentialCount != 1 {
		t.Fatalf("rotation lost version isolation: %+v", keys[0])
	}
	if err = state.GateFailure(t.Context(), "account", hold.AssociationRef, "", "rate", time.Minute); err != nil {
		t.Fatal(err)
	}
	path, vault := state.Path, state.Vault
	state.Close()
	state, err = store.Open(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	secret, err := state.Credential(t.Context(), hold.CredentialVersionId)
	if err != nil || secret != fakeProviderKey {
		t.Fatalf("pinned key lost: %v", err)
	}
	_, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("job"), PrincipalId: principal, Provider: "exa", Operation: "research_create", Amount: 50, Research: true})
	faultCode(t, err, "capacity_unavailable")
	_, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search", Amount: 10})
	faultCode(t, err, "provider_unavailable")
}

func TestCredentialAndAuditContainNoPlainSecret(t *testing.T) {
	state, principal := openStore(t)
	_, key := addAccount(t, state, principal, 100)
	var cipher []byte
	if err := state.DB.QueryRow("SELECT ciphertext FROM credential_versions WHERE key_id=?", key.Id).Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte(fakeProviderKey)) {
		t.Fatal("plaintext credential persisted")
	}
	rows, err := state.DB.Query("SELECT data FROM audit_events")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var data string
		rows.Scan(&data)
		if bytes.Contains([]byte(data), []byte(fakeProviderKey)) {
			t.Fatal("secret leaked to audit")
		}
	}
	rows.Close()
	state.Close()
	data, err := os.ReadFile(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(fakeProviderKey)) || bytes.Contains(data, []byte(fakeCredential)) {
		t.Fatal("credential marker in database")
	}
}

func TestStablePrincipalRevocationExpiryAndLoginLimit(t *testing.T) {
	state, principal := openStore(t)
	first, firstToken, err := state.Login(t.Context(), fakeCredential, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	second, secondToken, err := state.Login(t.Context(), fakeCredential, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if first.PrincipalId != principal || second.PrincipalId != principal || firstToken == secondToken {
		t.Fatal("session rotation changed identity or reused token")
	}
	created, err := state.MintToken(t.Context(), principal, domain.TokenCreate{Label: "Test", Scopes: []domain.ClientScope{"read"}, ExpiresAt: store.Stamp(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := state.AuthenticateToken(t.Context(), created.TokenValue)
	if err != nil || identity.PrincipalId != principal || len(identity.Permissions) != 0 {
		t.Fatalf("token identity: %+v %v", identity, err)
	}
	if _, err = state.RevokeToken(t.Context(), principal, created.Token.Id, 1); err != nil {
		t.Fatal(err)
	}
	_, err = state.AuthenticateToken(t.Context(), created.TokenValue)
	faultCode(t, err, "invalid_token")
	for index := 0; index < 10; index++ {
		_, _, err = state.Login(t.Context(), "wrong", "192.0.2.1")
		faultCode(t, err, "invalid_token")
	}
	_, _, err = state.Login(t.Context(), fakeCredential, "192.0.2.1")
	faultCode(t, err, "local_rate_limited")
	state.Now = func() time.Time { return time.Now().Add(9 * time.Hour) }
	_, err = state.Session(context.Background(), secondToken)
	faultCode(t, err, "authentication_required")
}
