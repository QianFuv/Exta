package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

type fixtureResolver struct{ calls atomic.Int32 }

func (resolver *fixtureResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.calls.Add(1)
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

type localTransport func(*http.Request) (*http.Response, error)

func (transport localTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func retrievalService(t *testing.T, state *store.Store, exa, tavily *mockProvider) (*service.Service, *fixtureResolver) {
	t.Helper()
	clients := map[domain.Provider]*providers.Client{}
	for provider, mock := range map[domain.Provider]*mockProvider{"exa": exa, "tavily": tavily} {
		if mock == nil {
			continue
		}
		address, _ := url.Parse(mock.Server.URL)
		transport := mock.Server.Client().Transport
		client, err := providers.New(provider, localTransport(func(request *http.Request) (*http.Response, error) {
			copied := request.Clone(request.Context())
			copied.URL.Scheme = address.Scheme
			copied.URL.Host = address.Host
			copied.Host = address.Host
			return transport.RoundTrip(copied)
		}))
		if err != nil {
			t.Fatal(err)
		}
		clients[provider] = client
	}
	resolver := &fixtureResolver{}
	core, err := service.New(state, clients, security.UrlPolicy{Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	core.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return core, resolver
}

func addTavily(t *testing.T, state *store.Store, principal string) domain.UpstreamKey {
	t.Helper()
	account, err := state.AddKey(t.Context(), principal, domain.UpstreamKeyCreate{Provider: "tavily", QuotaPolicy: (domain.QuotaPolicy{Unit: "tavily_credit", Allocation: 1000, PerOperationReservation: 10, ActiveJobs: 2}).Input(), Enabled: true, Label: "Synthetic Tavily", Secret: "synthetic-tavily-key-only"})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func assertCount(t *testing.T, state *store.Store, query string, want int) {
	t.Helper()
	var count int
	if err := state.DB.QueryRow(query).Scan(&count); err != nil || count != want {
		t.Fatalf("%s: got %d want %d (%v)", query, count, want, err)
	}
}

func TestRetrievalEmptySuccessDoesNotFallback(t *testing.T) {
	state, principal, _, _ := foundation(t)
	addTavily(t, state, principal)
	exa := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"costDollars":{"total":0}}`})
	tavily := newMockProvider(t)
	core, _ := retrievalService(t, state, exa, tavily)
	id := store.ID("request")
	result, err := core.Retrieve(t.Context(), principal, id, "search", []byte(`{"query":"private query must not be stored"}`))
	if err != nil || len(result.Results) != 0 || result.Partial || result.Meta.AttemptCount != 1 || len(tavily.Calls()) != 0 {
		t.Fatalf("empty result: %+v %v", result, err)
	}
	detail, err := state.Request(t.Context(), principal, id)
	if err != nil || detail.Request.ResultCount == nil || *detail.Request.ResultCount != 0 || detail.Request.BillingUncertain {
		t.Fatalf("summary: %+v %v", detail, err)
	}
	if strings.Contains(store.JSON(detail), "private query") {
		t.Fatal("query retained in request metadata")
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations WHERE state='settled'", 1)
}

func TestRetrievalSemanticRejectionsAndPreviewHaveNoAdmissionEffects(t *testing.T) {
	state, principal, _, _ := foundation(t)
	exa := newMockProvider(t)
	core, resolver := retrievalService(t, state, exa, nil)
	for _, fixture := range []struct{ operation, body string }{
		{"search", `{"query":"q","unknown":true}`},
		{"advanced_search", `{"query":"q","profile":"exa_deep","provider_options":{"kind":"exa_deep","category":"people","published_after":"2026-01-01T00:00:00Z"}}`},
		{"advanced_search", `{"query":"q","profile":"tavily_advanced","provider_options":{"kind":"tavily_advanced","country":"unsupported-place"}}`},
		{"fetch", `{"urls":["http://127.0.0.1/private"]}`},
		{"map", `{"url":"https://example.org","select_paths":["(?=unsafe)"]}`},
	} {
		if _, err := core.Retrieve(t.Context(), principal, store.ID("request"), fixture.operation, []byte(fixture.body)); err == nil {
			t.Fatalf("accepted unsupported intent: %s", fixture.body)
		}
	}
	preview, err := core.Preview(t.Context(), []byte(`{"operation":"fetch","request":{"urls":["https://example.org/a"]}}`))
	if err != nil || len(preview.Eligible) != 2 || resolver.calls.Load() != 0 || len(exa.Calls()) != 0 {
		t.Fatalf("preview effects: %+v %v DNS=%d", preview, err, resolver.calls.Load())
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
	assertCount(t, state, "SELECT count(*) FROM rate_windows", 0)
	assertCount(t, state, "SELECT count(*) FROM requests", 0)
	if err = domain.ValidateJSON("RoutePreview", []byte(store.JSON(preview))); err != nil {
		t.Fatal(err)
	}
}

func TestRetrievalThreeAttemptsAcrossKeysAndProviders(t *testing.T) {
	state, principal, account, _ := foundation(t)
	addTavily(t, state, principal)
	for _, secret := range []string{"synthetic-second-key", "synthetic-third-key"} {
		if _, err := addLinkedKey(t, state, principal, account, domain.UpstreamKeyCreate{Enabled: true, Label: "Additional", Secret: secret}); err != nil {
			t.Fatal(err)
		}
	}
	exa := newMockProvider(t, mockReply{Status: 401, Body: `{"error":"invalid"}`}, mockReply{Status: 401, Body: `{"error":"invalid"}`})
	tavily := newMockProvider(t, mockReply{Status: 401, Body: `{"detail":"invalid"}`})
	core, _ := retrievalService(t, state, exa, tavily)
	id := store.ID("request")
	_, err := core.Retrieve(t.Context(), principal, id, "search", []byte(`{"query":"q"}`))
	if err == nil || len(exa.Calls())+len(tavily.Calls()) != 3 {
		t.Fatalf("attempt budget violated: %v", err)
	}
	detail, err := state.Request(t.Context(), principal, id)
	if err != nil || len(detail.Attempts) != 3 {
		t.Fatalf("attempt history: %+v %v", detail, err)
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations WHERE state='released_unspent'", 3)
	assertCount(t, state, "SELECT count(*) FROM usage_ledger", 0)
}

func TestRetrievalFinancialUncertaintyRequiresExplicitDuplicateCostPolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "opt_in"}[allow], func(t *testing.T) {
			state, principal, account, _ := foundation(t)
			addTavily(t, state, principal)
			if allow {
				route, err := state.Route(t.Context(), "search")
				if err != nil {
					t.Fatal(err)
				}
				_, err = state.SaveRoute(t.Context(), principal, "search", domain.RouteReplace{ExpectedVersion: route.Revision, Targets: route.Targets, Enabled: true, MaxAttempts: 3, DeadlineSeconds: 30, AllowDuplicateCost: true})
				if err != nil {
					t.Fatal(err)
				}
			}
			exa := newMockProvider(t, mockReply{Status: 500, Body: `{"error":"uncertain"}`})
			tavily := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"usage":{"credits":1}}`})
			core, _ := retrievalService(t, state, exa, tavily)
			id := store.ID("request")
			_, err := core.Retrieve(t.Context(), principal, id, "search", []byte(`{"query":"q"}`))
			if (err == nil) != allow || len(exa.Calls()) != 1 || len(tavily.Calls()) != map[bool]int{true: 1, false: 0}[allow] {
				t.Fatalf("retry policy violated: %v", err)
			}
			detail, err := state.Request(t.Context(), principal, id)
			if err != nil || !detail.Request.BillingUncertain {
				t.Fatalf("uncertainty lost: %+v %v", detail, err)
			}
			if err = state.Write(t.Context(), func(tx *sql.Tx) error {
				used, held, err := store.BalanceTx(t.Context(), tx, account.Id)
				if used != 0 || held != 7000 {
					t.Errorf("ambiguous attempt refunded: %d/%d", used, held)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRetrievalPartialFetchRetriesOnlyFailedURLs(t *testing.T) {
	state, principal, _, _ := foundation(t)
	addTavily(t, state, principal)
	exa := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[{"url":"https://example.org/a","text":"A"}],"statuses":[{"id":"https://example.org/a","status":"success"},{"id":"https://example.org/b","status":"error","error":{"tag":"CRAWL_TIMEOUT","httpStatusCode":504}}],"costDollars":{"total":0.001}}`})
	tavily := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[{"url":"https://example.org/b","raw_content":"B"}],"failed_results":[],"usage":{"credits":1}}`})
	core, _ := retrievalService(t, state, exa, tavily)
	result, err := core.Retrieve(t.Context(), principal, store.ID("request"), "fetch", []byte(`{"urls":["https://example.org/a","https://example.org/b"]}`))
	if err != nil || len(result.Results) != 2 || len(result.Failures) != 0 || result.Partial || result.Meta.AttemptCount != 2 {
		t.Fatalf("partial merge: %+v %v", result, err)
	}
	var input struct {
		Urls []string `json:"urls"`
	}
	if len(tavily.Calls()) != 1 || json.Unmarshal(tavily.Calls()[0].Body, &input) != nil || len(input.Urls) != 1 || input.Urls[0] != "https://example.org/b" {
		t.Fatalf("successful URL retried: %+v", input)
	}
	if len(result.Meta.Usage) != 2 || result.Meta.Usage[0].Unit == result.Meta.Usage[1].Unit {
		t.Fatal("different accounting units collapsed")
	}
}

func TestRetrievalCooldownDoesNotBlockOtherProvider(t *testing.T) {
	state, principal, account, _ := foundation(t)
	addTavily(t, state, principal)
	exa := newMockProvider(t, mockReply{Status: 429, Body: `{"tag":"RATE_LIMIT_EXCEEDED"}`, Headers: map[string]string{"Retry-After": "3600"}})
	tavily := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"usage":{"credits":1}}`})
	core, _ := retrievalService(t, state, exa, tavily)
	core.Wait = func(context.Context, time.Duration) error {
		t.Error("account cooldown incorrectly became a global wait")
		return errors.New("unexpected wait")
	}
	result, err := core.Retrieve(t.Context(), principal, store.ID("request"), "search", []byte(`{"query":"q"}`))
	if err != nil || result.Meta.SourceProvider == nil || *result.Meta.SourceProvider != "tavily" {
		t.Fatalf("provider independence failed: %+v %v", result, err)
	}
	var until int64
	if err = state.DB.QueryRow("SELECT cooldown_ms FROM runtime_gates WHERE scope='association' AND entity_id=?", account.Id).Scan(&until); err != nil || until < state.Now().Add(59*time.Minute).UnixMilli() {
		t.Fatal("account Retry-After was not retained")
	}
}
