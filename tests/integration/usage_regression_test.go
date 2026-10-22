package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/service"
	"exta/internal/store"
)

type usageReadFunc func([]byte) (int, error)

func (read usageReadFunc) Read(buffer []byte) (int, error) { return read(buffer) }

func TestUsageSharedCooldownSurvivesAccountsAndCallerCancellation(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(store.JSON(disconnect), func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			first := addTavily(t, state, principal)
			verified := "verified"
			second, err := state.CreateAccount(t.Context(), principal, domain.AccountCreate{Provider: "tavily", Enabled: true, Label: "Second account", VerificationState: &verified, QuotaPolicy: domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 1000, PerOperationReservation: 1, ActiveJobs: 1}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = state.AddKey(t.Context(), principal, domain.KeyCreate{AccountId: second.Id, Enabled: true, Label: "Second", Secret: "synthetic-independent-usage-key"}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
			state.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			client, err := providers.New("tavily", localTransport(func(*http.Request) (*http.Response, error) {
				calls++
				body := io.NopCloser(strings.NewReader(`{"error":"limited"}`))
				if disconnect {
					body = io.NopCloser(usageReadFunc(func([]byte) (int, error) { cancel(); return 0, context.Canceled }))
				}
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"300"}}, Body: body}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			usage := service.UsageService{Store: state, Clients: map[domain.Provider]*providers.Client{"tavily": client}}
			input := domain.UsageRefresh{}
			if disconnect {
				input.AccountId = &first.Id
			}
			_, _ = usage.Refresh(ctx, input)
			if calls != 1 {
				t.Fatalf("shared limit bypassed: %d", calls)
			}
			now = now.Add(121 * time.Second)
			_, err = usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &second.Id})
			var fault *store.Fault
			if !errors.As(err, &fault) || fault.Code != "local_rate_limited" || fault.RetryAfterMs != 179000 || calls != 1 {
				t.Fatalf("shared hint lost: %v calls=%d", err, calls)
			}
		})
	}
}

func TestUsageProviderCooldownStopsSiblingsAndSurvivesRestart(t *testing.T) {
	state, principal, _, _ := foundation(t)
	account := addTavily(t, state, principal)
	if _, err := state.AddKey(t.Context(), principal, domain.KeyCreate{AccountId: account.Id, Label: "Sibling", Enabled: true, Secret: "synthetic-usage-sibling"}); err != nil {
		t.Fatal(err)
	}
	mock := newMockProvider(t, mockReply{Status: 429, Body: `{"error":"rate limit"}`, Headers: map[string]string{"Retry-After": "300"}})
	core, _ := retrievalService(t, state, nil, mock)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	state.Now = func() time.Time { return now }
	usage := service.UsageService{Store: state, Clients: core.Clients}
	result, err := usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &account.Id})
	if err != nil || len(mock.Calls()) != 1 || result.NextRefreshAt != store.Stamp(now.Add(300*time.Second)) {
		t.Fatalf("shared cooldown: %+v %v calls=%d", result, err, len(mock.Calls()))
	}
	path, vault := state.Path, state.Vault
	state.Close()
	reopened, err := store.OpenExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.Now = func() time.Time { return now.Add(121 * time.Second) }
	usage.Store = reopened
	_, err = usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &account.Id})
	var fault *store.Fault
	if !errors.As(err, &fault) || fault.Code != "local_rate_limited" || fault.RetryAfterMs != 179000 || len(mock.Calls()) != 1 {
		t.Fatalf("durable cooldown: %v", err)
	}
}

func TestUsageCooldownStartsAtEachActualDispatch(t *testing.T) {
	state, principal, _, _ := foundation(t)
	account := addTavily(t, state, principal)
	if _, err := state.AddKey(t.Context(), principal, domain.KeyCreate{AccountId: account.Id, Label: "Sibling", Enabled: true, Secret: "synthetic-delayed-sibling"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	start := now
	calls := 0
	state.Now = func() time.Time { return now }
	client, err := providers.New("tavily", localTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			now = now.Add(10 * time.Second)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"key":{"usage":0},"account":{}}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	usage := service.UsageService{Store: state, Clients: map[domain.Provider]*providers.Client{"tavily": client}}
	result, err := usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &account.Id})
	if err != nil || result.NextRefreshAt != store.Stamp(start.Add(130*time.Second)) {
		t.Fatalf("dispatch deadline: %+v %v", result, err)
	}
	now = start.Add(120 * time.Second)
	if _, err = usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &account.Id}); err == nil || calls != 2 {
		t.Fatalf("early refresh: %v calls=%d", err, calls)
	}
}

func TestUsageFailedSiblingDoesNotEraseAccountEvidence(t *testing.T) {
	for _, failureFirst := range []bool{false, true} {
		t.Run(store.JSON(failureFirst), func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			account := addTavily(t, state, principal)
			if _, err := state.AddKey(t.Context(), principal, domain.KeyCreate{AccountId: account.Id, Label: "Sibling", Enabled: true, Secret: "synthetic-failing-sibling"}); err != nil {
				t.Fatal(err)
			}
			success := mockReply{Status: 200, Body: `{"key":{"usage":1,"limit":10},"account":{"plan_usage":10,"paygo_usage":2,"plan_limit":100,"paygo_limit":100}}`}
			failure := mockReply{Status: 401, Body: `{"error":"invalid key"}`}
			replies := []mockReply{success, failure}
			if failureFirst {
				replies = []mockReply{failure, success}
			}
			mock := newMockProvider(t, replies...)
			core, _ := retrievalService(t, state, nil, mock)
			usage := service.UsageService{Store: state, Clients: core.Clients}
			if _, err := usage.Refresh(t.Context(), domain.UsageRefresh{AccountId: &account.Id}); err != nil {
				t.Fatal(err)
			}
			items, err := state.Usage(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			accounts, unknownKeys := 0, 0
			for _, item := range items {
				if item.AccountId != account.Id || item.Source != "upstream" {
					continue
				}
				if item.KeyId == nil {
					accounts++
					if item.Used == nil || *item.Used != 12 || item.LimitKind != "finite" {
						t.Fatalf("successful evidence erased: %+v", item)
					}
				} else if item.Used == nil {
					unknownKeys++
				}
			}
			if accounts != 1 || unknownKeys != 1 {
				t.Fatalf("rows: accounts=%d unknown keys=%d", accounts, unknownKeys)
			}
		})
	}
}

func TestUsageLocalAggregateRetainsEstimateProvenance(t *testing.T) {
	for _, certainties := range [][]string{{"reported"}, {"estimated"}, {"reported", "estimated"}} {
		t.Run(strings.Join(certainties, "-"), func(t *testing.T) {
			state, principal, account, _ := foundation(t)
			for _, certainty := range certainties {
				reservation, err := state.Reserve(t.Context(), store.Admission{ParentId: store.ID("request"), PrincipalId: principal, Provider: "exa", Operation: "search"})
				if err != nil {
					t.Fatal(err)
				}
				cost := int64(1)
				if err = state.Settle(t.Context(), reservation.Id, &cost, certainty); err != nil {
					t.Fatal(err)
				}
			}
			items, err := state.Usage(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			expected := "local"
			if strings.Contains(strings.Join(certainties, ","), "estimated") {
				expected = "estimate"
			}
			found := 0
			for _, item := range items {
				if item.AccountId == account.Id && item.Source != "upstream" {
					found++
					if item.Source != expected || item.Used == nil || int(*item.Used) != len(certainties) {
						t.Fatalf("aggregate provenance: %+v", item)
					}
				}
			}
			if found != 1 {
				t.Fatal("duplicate local aggregate")
			}
		})
	}
}
