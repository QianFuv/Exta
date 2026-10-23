package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

func routeOnly(t *testing.T, state *store.Store, principal, operation string, targets []domain.RouteTarget, duplicate bool, deadline domain.Integer) {
	t.Helper()
	route, err := state.Route(t.Context(), operation)
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.SaveRoute(t.Context(), principal, operation, domain.RouteReplace{ExpectedVersion: route.Revision, Enabled: true, Targets: targets, MaxAttempts: 3, DeadlineSeconds: deadline, AllowDuplicateCost: duplicate})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetrievalUnderfundedIntentNeverDispatches(t *testing.T) {
	for _, fixture := range []struct{ operation, body string }{
		{"advanced_search", `{"query":"q","profile":"tavily_advanced","provider_options":{"kind":"tavily_advanced"}}`},
		{"crawl", `{"url":"https://example.org","limit":50,"extraction_profile":"advanced"}`},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			account := addTavily(t, state, principal)
			quota := account.QuotaPolicy
			quota.Allocation = 1
			quota.PerOperationReservation = 1
			if _, err := state.PatchKey(t.Context(), principal, account.Id, domain.UpstreamKeyPatch{ExpectedVersion: account.Version, QuotaPolicy: &quota}); err != nil {
				t.Fatal(err)
			}
			profile := domain.Profile("common")
			if fixture.operation == "advanced_search" {
				profile = "tavily_advanced"
			}
			routeOnly(t, state, principal, fixture.operation, []domain.RouteTarget{{Provider: "tavily", Profile: profile}}, false, 60)
			mock := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"usage":{"credits":2}}`})
			core, _ := retrievalService(t, state, nil, mock)
			_, err := core.Retrieve(t.Context(), principal, store.ID("request"), fixture.operation, []byte(fixture.body))
			if err == nil || len(mock.Calls()) != 0 {
				t.Fatalf("underfunded dispatch: %v calls=%d", err, len(mock.Calls()))
			}
			assertCount(t, state, "SELECT count(*) FROM budget_reservations", 0)
		})
	}
}

func TestRetrievalPolicyRejectionBodyCannotAuthorizeFallback(t *testing.T) {
	for _, status := range []int{400, 403, 422} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			addTavily(t, state, principal)
			routeOnly(t, state, principal, "search", []domain.RouteTarget{{Provider: "exa", Profile: "common"}, {Provider: "tavily", Profile: "common"}}, true, 30)
			settings, _ := state.Settings(t.Context())
			limits := settings.Limits
			limits.ResponseBytes = 1024
			if _, err := state.SaveSettings(t.Context(), principal, domain.SettingsPatch{ExpectedVersion: settings.Version, Limits: &limits}); err != nil {
				t.Fatal(err)
			}
			exa := newMockProvider(t, mockReply{Status: status, Body: strings.Repeat("x", 2048)})
			tavily := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"usage":{"credits":1}}`})
			core, _ := retrievalService(t, state, exa, tavily)
			if _, err := core.Retrieve(t.Context(), principal, store.ID("request"), "search", []byte(`{"query":"q"}`)); err == nil || len(tavily.Calls()) != 0 {
				t.Fatalf("policy fallback: %v", err)
			}
		})
	}
}

type slowResolver struct{}

func (slowResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRetrievalRouteDeadlineIncludesDNS(t *testing.T) {
	state, principal, _, _ := foundation(t)
	routeOnly(t, state, principal, "fetch", []domain.RouteTarget{{Provider: "exa", Profile: "common"}}, false, 1)
	exa := newMockProvider(t)
	core, _ := retrievalService(t, state, exa, nil)
	core.URLs = security.UrlPolicy{Resolver: slowResolver{}}
	started := time.Now()
	_, err := core.Retrieve(t.Context(), principal, store.ID("request"), "fetch", []byte(`{"urls":["https://example.org/a"]}`))
	if err == nil || time.Since(started) > 2*time.Second || len(exa.Calls()) != 0 {
		t.Fatalf("route deadline omitted DNS: %v %s", err, time.Since(started))
	}
}

func TestRetrievalMarkdownAndOptionalProfilesPreserveIntent(t *testing.T) {
	for _, fixture := range []struct{ operation, profile, body string }{
		{"advanced_search", "exa_deep", `{"query":"q","profile":"exa_deep"}`},
		{"advanced_search", "tavily_advanced", `{"query":"q","profile":"tavily_advanced"}`},
		{"search", "common", `{"query":"q","content_mode":"markdown"}`},
		{"fetch", "common", `{"urls":["https://example.org/a"],"format":"markdown"}`},
	} {
		t.Run(fixture.operation+fixture.profile+fixture.body, func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			addTavily(t, state, principal)
			targets := []domain.RouteTarget{{Provider: "exa", Profile: "common"}, {Provider: "tavily", Profile: "common"}}
			provider := domain.Provider("tavily")
			if fixture.profile != "common" {
				if fixture.profile == "exa_deep" {
					provider = "exa"
				}
				targets = []domain.RouteTarget{{Provider: provider, Profile: domain.Profile(fixture.profile)}}
			}
			routeOnly(t, state, principal, fixture.operation, targets, false, 30)
			exa := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"costDollars":{"total":0.012}}`})
			tavily := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[{"url":"https://example.org/a","raw_content":"# A"}],"usage":{"credits":2}}`})
			core, _ := retrievalService(t, state, exa, tavily)
			preview, err := core.Preview(t.Context(), []byte(store.JSON(map[string]any{"operation": fixture.operation, "request": json.RawMessage(fixture.body)})))
			if err != nil || len(preview.Eligible) != 1 || preview.Eligible[0].Provider != provider {
				t.Fatalf("preview: %+v %v", preview, err)
			}
			_, err = core.Retrieve(t.Context(), principal, store.ID("request"), fixture.operation, []byte(fixture.body))
			if err != nil {
				t.Fatal(err)
			}
			if provider == "tavily" && len(exa.Calls()) != 0 {
				t.Fatal("Markdown/profile sent to incompatible Exa")
			}
		})
	}
}

func TestRetrievalFailedRetryReplacesPerURLClassification(t *testing.T) {
	for _, status := range []int{403, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			state, principal, _, _ := foundation(t)
			addTavily(t, state, principal)
			exa := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[{"url":"https://example.org/a","text":"A"}],"statuses":[{"id":"https://example.org/b","status":"error","error":{"tag":"CRAWL_TIMEOUT"}}],"costDollars":{"total":0.001}}`})
			tavily := newMockProvider(t, mockReply{Status: status, Body: `{"detail":"rejected"}`})
			core, _ := retrievalService(t, state, exa, tavily)
			result, err := core.Retrieve(t.Context(), principal, store.ID("request"), "fetch", []byte(`{"urls":["https://example.org/a","https://example.org/b"]}`))
			if err != nil || len(result.Results) != 1 || len(result.Failures) != 1 || result.Failures[0].Provider != "tavily" || result.Failures[0].Retryable {
				t.Fatalf("stale URL failure: %+v %v", result, err)
			}
		})
	}
}

func TestRetrievalEntireFetchFailureIsNotSuccess(t *testing.T) {
	state, principal, _, _ := foundation(t)
	addTavily(t, state, principal)
	routeOnly(t, state, principal, "fetch", []domain.RouteTarget{{Provider: "tavily", Profile: "common"}}, false, 60)
	mock := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"failed_results":[{"url":"https://example.org/a","error":"blocked"}],"usage":{"credits":0}}`})
	core, _ := retrievalService(t, state, nil, mock)
	id := store.ID("request")
	_, err := core.Retrieve(t.Context(), principal, id, "fetch", []byte(`{"urls":["https://example.org/a"]}`))
	var fault *store.Fault
	if !errors.As(err, &fault) {
		t.Fatalf("all failed returned success: %v", err)
	}
	detail, err := state.Request(t.Context(), principal, id)
	if err != nil || detail.Request.Status != "failed" {
		t.Fatalf("misleading history %+v %v", detail, err)
	}
}

func TestRetrievalSharedRateLimitSurvivesAccountChangeAndRestart(t *testing.T) {
	state, principal, _, _ := foundation(t)
	_, err := state.AddKey(t.Context(), principal, domain.UpstreamKeyCreate{Provider: "exa", QuotaPolicy: &domain.QuotaPolicy{Unit: "micro_usd", Allocation: 1000000, PerOperationReservation: 1000, ActiveJobs: 2}, Label: "Other team", Enabled: true, Secret: "synthetic-other-account-key"})
	if err != nil {
		t.Fatal(err)
	}
	mock := newMockProvider(t, mockReply{Status: 429, Body: `{"error":"shared endpoint throttle"}`, Headers: map[string]string{"Retry-After": "120"}})
	core, _ := retrievalService(t, state, mock, nil)
	if _, err = core.Retrieve(t.Context(), principal, store.ID("request"), "search", []byte(`{"query":"q"}`)); err == nil {
		t.Fatal("rate limit returned success")
	}
	path, vault := state.Path, state.Vault
	state.Close()
	reopened, err := store.OpenExisting(t.Context(), path, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	core, _ = retrievalService(t, reopened, mock, nil)
	if _, err = core.Retrieve(t.Context(), principal, store.ID("request"), "search", []byte(`{"query":"q"}`)); err == nil || len(mock.Calls()) != 1 {
		t.Fatalf("shared limit bypass: %v calls=%d", err, len(mock.Calls()))
	}
}

func TestRetrievalRejectsUnconfiguredReservationAndEmptyStatusEvidence(t *testing.T) {
	for _, emptyStatus := range []bool{false, true} {
		t.Run(strconv.FormatBool(emptyStatus), func(t *testing.T) {
			state, principal, account, _ := foundation(t)
			if !emptyStatus {
				quota := account.QuotaPolicy
				quota.PerOperationReservation = 0
				if _, err := state.PatchKey(t.Context(), principal, account.Id, domain.UpstreamKeyPatch{ExpectedVersion: account.Version, QuotaPolicy: &quota}); err != nil {
					t.Fatal(err)
				}
			}
			mock := newMockProvider(t, mockReply{Status: 200, Body: `{"results":[],"statuses":[{"id":"https://example.org/a","status":"success"}],"costDollars":{"total":0.001}}`})
			core, _ := retrievalService(t, state, mock, nil)
			_, err := core.Retrieve(t.Context(), principal, store.ID("request"), "fetch", []byte(`{"urls":["https://example.org/a"]}`))
			if err == nil {
				t.Fatal("unconfigured or empty retrieval accepted")
			}
			if !emptyStatus && len(mock.Calls()) != 0 {
				t.Fatal("zero reservation replaced by price floor")
			}
		})
	}
}

func TestRetrievalConcurrentAdmissionReservesFullSelectedCost(t *testing.T) {
	state, principal, _, _ := foundation(t)
	account := addTavily(t, state, principal)
	quota := account.QuotaPolicy
	quota.Allocation = 3
	quota.PerOperationReservation = 1
	if _, err := state.PatchKey(t.Context(), principal, account.Id, domain.UpstreamKeyPatch{ExpectedVersion: account.Version, QuotaPolicy: &quota}); err != nil {
		t.Fatal(err)
	}
	routeOnly(t, state, principal, "advanced_search", []domain.RouteTarget{{Provider: "tavily", Profile: "tavily_advanced"}}, false, 30)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	client, err := providers.New("tavily", localTransport(func(request *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"results":[],"usage":{"credits":2}}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	core, err := service.New(state, map[domain.Provider]*providers.Client{"tavily": client}, security.UrlPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		_, err := core.Retrieve(t.Context(), principal, store.ID("request"), "advanced_search", []byte(`{"query":"q","profile":"tavily_advanced"}`))
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first request did not reach provider")
	}
	_, secondErr := core.Retrieve(t.Context(), principal, store.ID("request"), "advanced_search", []byte(`{"query":"q2","profile":"tavily_advanced"}`))
	close(release)
	if err = <-first; err != nil {
		t.Fatal(err)
	}
	if secondErr == nil || len(entered) != 0 {
		t.Fatalf("concurrent over-allocation: %v additional calls=%d", secondErr, len(entered))
	}
	assertCount(t, state, "SELECT count(*) FROM budget_reservations", 1)
}
