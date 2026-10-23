package store_test

import (
	"exta/internal/domain"
	"exta/internal/store"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirectKeysNeedNoParentOrVerifiedIdentity(t *testing.T) {
	state, owner := openStore(t)
	var obsolete int
	if err := state.DB.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('provider_accounts','legacy_provider_accounts','legacy_record_payloads')").Scan(&obsolete); err != nil || obsolete != 0 {
		t.Fatalf("obsolete parent schema: %d %v", obsolete, err)
	}
	quota := domain.QuotaPolicy{Unit: "micro_usd", Allocation: 10, PerOperationReservation: 10, ActiveJobs: 1}
	keys := []domain.UpstreamKey{}
	for _, secret := range []string{"synthetic-independent-one", "synthetic-independent-two"} {
		key, err := state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", Label: "Independent", Secret: secret, Enabled: true, QuotaPolicy: &quota})
		if err != nil {
			t.Fatal(err)
		}
		if key.AssociationEvidence != "unknown" || key.AssociationRef != key.Id || key.SharedLimit != nil {
			t.Fatal("invented shared identity")
		}
		keys = append(keys, key)
	}
	selected := map[string]bool{}
	for range 2 {
		hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"})
		if err != nil {
			t.Fatal(err)
		}
		selected[hold.KeyId] = true
	}
	if len(selected) != 2 {
		t.Fatal("configured unknown-identity keys did not admit independently")
	}
	_, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"})
	faultCode(t, err, "quota_unavailable")
	_, err = state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", Label: "Duplicate", Secret: "synthetic-independent-one", Enabled: true, QuotaPolicy: &quota})
	faultCode(t, err, "invalid_request")
	quota.Allocation = 0
	_, err = state.PatchKey(t.Context(), owner, keys[0].Id, domain.UpstreamKeyPatch{ExpectedVersion: keys[0].Version, QuotaPolicy: &quota})
	faultCode(t, err, "invalid_request")
}

func TestKeyMetadataShowsEnforcedSharedGate(t *testing.T) {
	state, owner := openStore(t)
	key, _ := addAccount(t, state, owner, 100)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	state.Now = func() time.Time { return now }
	if _, err := state.DB.Exec("UPDATE runtime_gates SET blocked=1,cooldown_ms=?,breaker_ms=? WHERE scope='association' AND entity_id=?", now.Add(time.Minute).UnixMilli(), now.Add(time.Minute).UnixMilli(), key.AssociationRef); err != nil {
		t.Fatal(err)
	}
	keys, err := state.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].BillingState != "blocked" || keys[0].Breaker != "open" || keys[0].CooldownUntil == nil {
		t.Fatalf("enforced shared gate hidden: %+v", keys)
	}
}

func TestLinkedKeysShareOneReservationCeiling(t *testing.T) {
	state, owner := openStore(t)
	quota := domain.QuotaPolicy{Unit: "micro_usd", Allocation: 100, PerOperationReservation: 10, ActiveJobs: 2}
	shared := quota
	shared.Allocation = 30
	keys := []domain.UpstreamKey{}
	for _, secret := range []string{"synthetic-linked-one", "synthetic-linked-two"} {
		key, err := state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", Label: "Linked", Secret: secret, Enabled: true, QuotaPolicy: &quota})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	for index := range keys {
		keys[index].AssociationRef = keys[0].Id
		keys[index].AssociationEvidence = "operator_declared"
		keys[index].SharedLimit = &shared
		if _, err := state.DB.Exec("UPDATE provider_keys SET association_ref=?,data=? WHERE id=?", keys[0].Id, store.JSON(keys[index]), keys[index].Id); err != nil {
			t.Fatal(err)
		}
	}
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			if _, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"}); err == nil {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("shared limit duplicated or unavailable: %d", accepted.Load())
	}
	secret := "synthetic-linked-rotated"
	if _, err := state.ChangeKey(t.Context(), owner, keys[0].Id, keys[0].Version, nil, nil, &secret, false); err != nil {
		t.Fatal(err)
	}
	_, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search"})
	faultCode(t, err, "quota_unavailable")
	rows, err := state.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	sharedRows := 0
	for _, row := range rows {
		if row.Source == "local" && row.Scope == "shared" {
			sharedRows++
			if row.Reserved != 30 {
				t.Fatalf("lost shared holds: %+v", row)
			}
		}
	}
	if sharedRows != 1 {
		t.Fatalf("duplicated shared balance: %d", sharedRows)
	}
}
