package store_test

import (
	"fmt"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestDelayedSuccessCannotCloseNewerCircuit(t *testing.T) {
	for _, scope := range []string{"account", "key"} {
		t.Run(scope, func(t *testing.T) {
			state, principal := openStore(t)
			account, key := addAccount(t, state, principal, 100)
			now := time.Now()
			state.Now = func() time.Time { return now }
			old, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
			if err != nil {
				t.Fatal(err)
			}
			id := account.Id
			if scope == "key" {
				id = key.Id
			}
			for index := 0; index < 5; index++ {
				if err = state.GateFailure(t.Context(), scope, id, old.CredentialVersionId, "transient", 0); err != nil {
					t.Fatal(err)
				}
			}
			if err = state.GateSuccess(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			input := store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}
			if _, err = state.Reserve(t.Context(), input); err == nil {
				t.Fatal("stale success bypassed open interval")
			}
			now = now.Add(31 * time.Second)
			probe, err := state.Reserve(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if err = state.GateSuccess(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			input.ParentId = store.ID("request")
			if _, err = state.Reserve(t.Context(), input); err == nil {
				t.Fatal("stale success released newer half-open probe")
			}
			now = now.Add(181 * time.Second)
			takeover, err := state.Reserve(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if err = state.GateSuccess(t.Context(), probe); err != nil {
				t.Fatal(err)
			}
			input.ParentId = store.ID("request")
			if _, err = state.Reserve(t.Context(), input); err == nil {
				t.Fatal("expired probe released its replacement")
			}
			if err = state.GateSuccess(t.Context(), takeover); err != nil {
				t.Fatal(err)
			}
			if _, err = state.Reserve(t.Context(), input); err != nil {
				t.Fatal("own probe could not close circuit", err)
			}
		})
	}
}

func TestRetirementPreservesPinnedCredentialAndPreventsNewAdmission(t *testing.T) {
	state, principal := openStore(t)
	_, key := addAccount(t, state, principal, 100)
	hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("job"), PrincipalId: principal, Provider: "exa", Operation: "research_create", Amount: 10, Research: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.ChangeKey(t.Context(), principal, key.Id, 1, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	keys, err := state.Keys(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].Enabled || keys[0].RetirementState != "retiring" || keys[0].RetainedCredentialCount != 1 {
		t.Fatalf("retirement metadata: %+v", keys[0])
	}
	secret, err := state.Credential(t.Context(), hold.CredentialVersionId)
	if err != nil || secret != fakeProviderKey {
		t.Fatal("retirement erased ongoing credential", err)
	}
	if _, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err == nil {
		t.Fatal("retired key admitted new work")
	}
}

func TestAccountKeyFairnessAndProviderIsolation(t *testing.T) {
	state, principal := openStore(t)
	accountCounts := map[string]int{}
	keyCounts := map[string]int{}
	for index := 0; index < 2; index++ {
		var first domain.UpstreamKey
		for keyIndex := 0; keyIndex < 2; keyIndex++ {
			key, err := state.AddKey(t.Context(), principal, domain.UpstreamKeyCreate{Provider: "exa", Label: fmt.Sprintf("Key %d", keyIndex), Enabled: true, Secret: fmt.Sprintf("synthetic-fairness-%d-%d", index, keyIndex), QuotaPolicy: (domain.QuotaPolicy{Unit: "micro_usd", Allocation: 100, PerOperationReservation: 1, ActiveJobs: 2}).Input()})
			if err != nil {
				t.Fatal(err)
			}
			if keyIndex == 0 {
				first = key
			}
			key.AssociationRef = first.AssociationRef
			key.AssociationEvidence = "operator_declared"
			key.SharedLimit = &first.QuotaPolicy
			if _, err = state.DB.Exec("UPDATE provider_keys SET association_ref=?,data=? WHERE id=?", key.AssociationRef, store.JSON(key), key.Id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for index := 0; index < 8; index++ {
		hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
		if err != nil {
			t.Fatal(err)
		}
		accountCounts[hold.AssociationRef]++
		keyCounts[hold.KeyId]++
	}
	if len(accountCounts) != 2 || len(keyCounts) != 4 {
		t.Fatalf("pool starvation: %v %v", accountCounts, keyCounts)
	}
	for _, count := range accountCounts {
		if count != 4 {
			t.Fatal("accounts not fair")
		}
	}
	for _, count := range keyCounts {
		if count != 2 {
			t.Fatal("keys not fair")
		}
	}
	for account := range accountCounts {
		if err := state.GateFailure(t.Context(), "account", account, "", "quota", 0); err != nil {
			t.Fatal(err)
		}
	}
	_, err := state.AddKey(t.Context(), principal, domain.UpstreamKeyCreate{Provider: "tavily", Label: "Independent", Enabled: true, Secret: "synthetic-independent-tavily", QuotaPolicy: (domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 100, PerOperationReservation: 1, ActiveJobs: 1}).Input()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "tavily", Operation: "search"}); err != nil {
		t.Fatal("Exa account blocks contaminated Tavily", err)
	}
}

func TestDelayedKeyQuotaDoesNotBlockRotatedCredential(t *testing.T) {
	state, principal := openStore(t)
	_, key := addAccount(t, state, principal, 100)
	old, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-quota-rotation-regression"
	if _, err = state.ChangeKey(t.Context(), principal, key.Id, 1, nil, nil, &secret, false); err != nil {
		t.Fatal(err)
	}
	if err = state.GateFailure(t.Context(), "key", key.Id, old.CredentialVersionId, "quota", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err != nil {
		t.Fatal("old key quota blocked replacement", err)
	}
}

func TestIdenticalSecretRotationCannotEraseRejection(t *testing.T) {
	for _, kind := range []string{"invalid", "quota"} {
		t.Run(kind, func(t *testing.T) {
			state, principal := openStore(t)
			_, key := addAccount(t, state, principal, 100)
			hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
			if err != nil {
				t.Fatal(err)
			}
			if err = state.GateFailure(t.Context(), "key", key.Id, hold.CredentialVersionId, kind, 0); err != nil {
				t.Fatal(err)
			}
			secret := fakeProviderKey
			if _, err = state.ChangeKey(t.Context(), principal, key.Id, 1, nil, nil, &secret, false); err == nil {
				t.Fatal("identical rejected secret rotated")
			}
			path, vault := state.Path, state.Vault
			state.Close()
			reopened, err := store.Open(t.Context(), path, vault)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if _, err = reopened.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err == nil {
				t.Fatal("unchanged secret became eligible")
			}
		})
	}
}

func TestLowRateRefillsEntireBurstAfterLongIdle(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "in_process", true: "reopened"}[restart], func(t *testing.T) {
			state, principal := openStore(t)
			addAccount(t, state, principal, 1000)
			now := time.Now()
			state.Now = func() time.Time { return now }
			settings, err := state.Settings(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			settings.Limits.PrincipalRequestsPerMinute = 1
			if _, err = state.DB.Exec("UPDATE settings SET data=?", store.JSON(settings)); err != nil {
				t.Fatal(err)
			}
			admit := func() {
				t.Helper()
				if _, err = state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"}); err != nil {
					t.Fatal(err)
				}
			}
			for index := 0; index < 10; index++ {
				admit()
			}
			now = now.Add(10 * time.Minute)
			if restart {
				path, vault := state.Path, state.Vault
				state.Close()
				state, err = store.Open(t.Context(), path, vault)
				if err != nil {
					t.Fatal(err)
				}
				defer state.Close()
				state.Now = func() time.Time { return now }
			}
			for index := 0; index < 10; index++ {
				admit()
			}
		})
	}
}
