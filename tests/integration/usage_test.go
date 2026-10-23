package integration

import (
	"errors"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/service"
	"exta/internal/store"
)

func TestUsageSnapshotsDoNotSumSharedAccountsAndCooldownSurvivesRestart(t *testing.T) {
	state, principal, _, _ := foundation(t)
	account := addTavily(t, state, principal)
	if _, err := addLinkedKey(t, state, principal, account, domain.UpstreamKeyCreate{Label: "Second key same account", Enabled: true, Secret: "synthetic-usage-second-key"}); err != nil {
		t.Fatal(err)
	}
	mock := newMockProvider(t, mockReply{Status: 200, Body: `{"key":{"usage":1,"limit":null},"account":{"plan_usage":10,"paygo_usage":2,"plan_limit":100,"paygo_limit":100}}`}, mockReply{Status: 200, Body: `{"key":{"usage":2,"limit":10},"account":{"plan_usage":10,"paygo_usage":2,"plan_limit":100,"paygo_limit":100}}`})
	core, _ := retrievalService(t, state, nil, mock)
	usage := service.UsageService{Store: state, Clients: core.Clients}
	result, err := usage.Refresh(t.Context(), domain.KeyUsageRefresh{})
	if err != nil || result.Status != "refreshed" || len(mock.Calls()) != 2 {
		t.Fatalf("refresh: %+v %v", result, err)
	}
	items, err := state.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	accountRows, keyRows := 0, 0
	for _, item := range items {
		if err = domain.ValidateJSON("KeyUsage", []byte(store.JSON(item))); err != nil {
			t.Fatal(err)
		}
		if item.AssociationRef != account.Id || item.Source != "upstream" {
			continue
		}
		if item.KeyId == nil {
			accountRows++
			if item.Used == nil || *item.Used != 12 {
				t.Fatal("account usage counted twice")
			}
		} else {
			keyRows++
		}
	}
	if accountRows != 1 || keyRows != 2 {
		t.Fatalf("snapshot rows %d/%d", accountRows, keyRows)
	}
	assertCount(t, state, "SELECT count(*) FROM usage_ledger", 0)
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
	path, vault := state.Path, state.Vault
	state.Close()
	reopened, err := store.OpenExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	usage.Store = reopened
	_, err = usage.Refresh(t.Context(), domain.KeyUsageRefresh{})
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Code != "local_rate_limited" || fault.RetryAfterMs <= 60000 || len(mock.Calls()) != 2 {
		t.Fatalf("cooldown lost %v", err)
	}
	reopened.Now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	items, err = reopened.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Source == "upstream" && item.AssociationRef == account.Id && item.Freshness != "stale" {
			t.Fatal("snapshot freshness invented")
		}
	}
}

func TestUsageExaWithoutServiceReferenceNeverUsesPoolKey(t *testing.T) {
	state, principal, account, _ := foundation(t)
	exa := newMockProvider(t)
	core, _ := retrievalService(t, state, exa, nil)
	usage := service.UsageService{Store: state, Clients: core.Clients, ExaClient: core.Clients["exa"]}
	if _, err := usage.Refresh(t.Context(), domain.KeyUsageRefresh{KeyId: &account.Id}); err != nil {
		t.Fatal(err)
	}
	items, err := state.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.KeyId != nil && item.Source == "upstream" {
			found = true
			if item.Used != nil || item.UnavailableReason == nil || *item.UnavailableReason != "service_credential_not_configured" {
				t.Fatal("missing service reference became a balance")
			}
		}
	}
	if !found || len(exa.Calls()) != 0 {
		t.Fatal("pooled key substituted")
	}
	key, err := addLinkedKey(t, state, principal, account, domain.UpstreamKeyCreate{Label: "Dedicated usage", Enabled: false, Secret: "synthetic-service-key-only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UsageCredential(t.Context(), key.Id, true); err != nil {
		t.Fatal(err)
	}
	if _, err = state.UsageCredential(t.Context(), key.Id, false); err == nil {
		t.Fatal("dedicated key entered ordinary usage pool")
	}
}
