package store_test

import (
	"encoding/json"
	"testing"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestNullableKeyPolicyAndResetKeepPositiveCaps(t *testing.T) {
	for _, policy := range []string{"null", "{}", `{"allocation":null,"active_jobs":null,"per_operation_reservation":null}`, `{"allocation":0}`} {
		state, owner := openStore(t)
		var input domain.UpstreamKeyCreate
		data := []byte(`{"provider":"tavily","label":"Synthetic","enabled":true,"secret":"synthetic-null-policy","quota_policy":` + policy + `}`)
		if err := json.Unmarshal(data, &input); err != nil {
			t.Fatal(err)
		}
		key, err := state.AddKey(t.Context(), owner, input)
		if err != nil || key.QuotaPolicy.Unit != "tavily_credit" || key.QuotaPolicy.Allocation != 0 {
			t.Fatalf("normalize %s: %+v %v", policy, key, err)
		}
		quota := domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 1}
		key, err = state.PatchKey(t.Context(), owner, key.Id, domain.UpstreamKeyPatch{ExpectedVersion: key.Version, QuotaPolicy: quota.Input()})
		if err != nil {
			t.Fatal(err)
		}
		if available, err := state.Available(t.Context(), "tavily", 2); err != nil || available {
			t.Fatalf("positive cap ignored: %v %v", available, err)
		}
		var patch domain.UpstreamKeyPatch
		if err = json.Unmarshal([]byte(`{"expected_version":2,"quota_policy":null}`), &patch); err != nil {
			t.Fatal(err)
		}
		if _, err = state.PatchKey(t.Context(), owner, key.Id, patch); err != nil {
			t.Fatal(err)
		}
		if available, err := state.Available(t.Context(), "tavily", 2); err != nil || !available {
			t.Fatalf("null reset blocked: %v %v", available, err)
		}
	}
}

func TestUnconfiguredLocalLimitAllowsUnknownUpstreamAndKeepsAccounting(t *testing.T) {
	state, owner := openStore(t)
	key, err := state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", Label: "No local cap", Secret: "synthetic-unrestricted-key", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	available, err := state.Available(t.Context(), "exa", 7000)
	if err != nil || !available {
		t.Fatalf("unconfigured local cap hides availability: %v %v", available, err)
	}
	hold, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: owner, Provider: "exa", Operation: "search", MinimumAmount: 7000})
	if err != nil || hold.Amount != 7000 {
		t.Fatalf("unconfigured local cap rejects dispatch or loses estimate: %+v %v", hold, err)
	}
	cost := int64(4000)
	if err = state.Settle(t.Context(), hold.Id, &cost, "actual"); err != nil {
		t.Fatal(err)
	}
	if err = state.Settle(t.Context(), hold.Id, &cost, "actual"); err != nil {
		t.Fatal(err)
	}
	rows, err := state.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.KeyId == nil || *row.KeyId != key.Id {
			continue
		}
		if row.Source == "upstream" {
			if row.LimitKind != "unknown" || row.Limit != nil || row.Remaining != nil {
				t.Fatalf("fabricated upstream balance: %+v", row)
			}
		} else if row.LimitKind != "unlimited" || row.Limit != nil || row.Remaining != nil || row.Used == nil || *row.Used != 4000 {
			t.Fatalf("local cap or settlement semantics: %+v", row)
		}
	}
}
