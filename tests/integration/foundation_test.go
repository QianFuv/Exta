package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

func foundation(t *testing.T) (*store.Store, string, domain.Account, domain.ProviderKey) {
	t.Helper()
	vault, err := security.NewVault(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	principal, err := state.BootstrapAdmin(t.Context(), "synthetic-local-operator-for-foundation")
	if err != nil {
		t.Fatal(err)
	}
	account, err := state.CreateAccount(t.Context(), principal, domain.AccountCreate{Provider: "exa", Enabled: true, Label: "Synthetic", QuotaPolicy: domain.QuotaPolicy{Unit: "micro_usd", Allocation: 1000000, PerOperationReservation: 1, ActiveJobs: 2}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := state.AddKey(t.Context(), principal, domain.KeyCreate{AccountId: account.Id, Label: "Synthetic", Enabled: true, Secret: "synthetic-provider-for-foundation"})
	if err != nil {
		t.Fatal(err)
	}
	return state, principal, account, key
}

func TestFoundationAccountHoldDominatesKeyRotation(t *testing.T) {
	state, principal, account, key := foundation(t)
	first, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.GateFailure(t.Context(), "account", account.Id, first.CredentialVersionId, "quota", 0); err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-key-after-quota-exhaustion"
	if _, err = state.ChangeKey(t.Context(), principal, key.Id, 1, nil, nil, &secret, false); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err == nil {
		t.Fatal("key rotation bypassed account quota hold")
	}
	keys, err := state.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].Validity == "invalid" {
		t.Fatal("quota failure mislabeled credential invalid")
	}
}

func TestFoundationCircuitHalfOpenAndRestart(t *testing.T) {
	state, principal, account, key := foundation(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	state.Now = func() time.Time { return now }
	for index := 0; index < 5; index++ {
		if err := state.GateFailure(t.Context(), "account", account.Id, "", "transient", 0); err != nil {
			t.Fatal(err)
		}
	}
	input := store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}
	if _, err := state.Reserve(t.Context(), input); err == nil {
		t.Fatal("open breaker dispatched")
	}
	now = now.Add(31 * time.Second)
	probe, err := state.Reserve(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.ParentId = store.ID("request")
	if _, err = state.Reserve(t.Context(), input); err == nil {
		t.Fatal("second half-open probe dispatched")
	}
	if err = state.GateSuccess(t.Context(), probe); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Reserve(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err = state.GateFailure(t.Context(), "key", key.Id, probe.CredentialVersionId, "rate", time.Hour); err != nil {
		t.Fatal(err)
	}
	path, vault := state.Path, state.Vault
	state.Close()
	reopened, err := store.Open(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.Now = func() time.Time { return now }
	if _, err = reopened.Reserve(t.Context(), input); err == nil {
		t.Fatal("restart erased key cooldown")
	}
}

func TestFoundationRatesAndRejectedReservationAreAtomic(t *testing.T) {
	state, principal, account, _ := foundation(t)
	now := time.Now()
	state.Now = func() time.Time { return now }
	for index := 0; index < 10; index++ {
		if _, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Code != "local_rate_limited" || fault.RetryAfterMs <= 0 {
		t.Fatalf("rate: %v", err)
	}
	var count int
	if err = state.DB.QueryRow("SELECT count(*) FROM budget_reservations").Scan(&count); err != nil || count != 10 {
		t.Fatalf("rejection created hold: %d %v", count, err)
	}
	quota := account.QuotaPolicy
	quota.Allocation = 9
	if _, err = state.PatchAccount(t.Context(), principal, account.Id, domain.AccountPatch{ExpectedVersion: 1, QuotaPolicy: &quota}); err == nil {
		t.Fatal("quota edit erased held budget")
	}
}

func TestFoundationRoutesRemainImmutableAcrossRestart(t *testing.T) {
	state, principal, _, _ := foundation(t)
	original, err := state.Route(t.Context(), "research_create")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := state.SaveRoute(t.Context(), principal, "research_create", domain.RouteReplace{ExpectedVersion: 1, Enabled: false, Targets: original.Targets, MaxAttempts: original.MaxAttempts, DeadlineSeconds: original.DeadlineSeconds})
	if err != nil || changed.Revision != 2 {
		t.Fatalf("route edit %v", err)
	}
	var frozen string
	if err = state.DB.QueryRow("SELECT data FROM route_policies WHERE capability='research_create' AND revision=1").Scan(&frozen); err != nil {
		t.Fatal(err)
	}
	var before domain.RoutePolicy
	if err = json.Unmarshal([]byte(frozen), &before); err != nil || !before.Enabled {
		t.Fatal("route edit changed original revision")
	}
	path, vault := state.Path, state.Vault
	state.Close()
	reopened, err := store.Open(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err := reopened.Route(t.Context(), "research_create")
	if err != nil || current.Revision != 2 || current.Enabled {
		t.Fatal("restart changed route")
	}
}

func TestFoundationFailedTransactionLeavesNoAdmission(t *testing.T) {
	state, principal, account, _ := foundation(t)
	sentinel := errors.New("simulate failure after reservation")
	err := state.Write(t.Context(), func(tx *sql.Tx) error {
		settings, err := store.SettingsTx(t.Context(), tx)
		if err != nil {
			return err
		}
		if _, err = state.ReserveTx(t.Context(), tx, store.Admission{ParentId: store.ID("job"), PrincipalId: principal, Provider: "exa", Operation: "research_create", Amount: 100, Research: true}, settings.Limits); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err = state.Write(t.Context(), func(tx *sql.Tx) error {
		used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
		if used != 0 || held != 0 {
			t.Errorf("partial transaction changed accounting %d/%d", used, held)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = state.DB.QueryRow("SELECT count(*) FROM rate_windows").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction consumed rate allowance")
	}
}
