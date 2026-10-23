package providers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"exta/internal/domain"
	"exta/internal/store"
)

func TestTavilyUsageDistinguishesUnknownUnlimitedAndZero(t *testing.T) {
	for _, fixture := range []struct{ body, kind string }{
		{`{"key":{"usage":0,"limit":null},"account":{"plan_usage":4,"paygo_usage":2,"plan_limit":10,"paygo_limit":10},"response_time":0.25}`, "unlimited"},
		{`{"key":{"usage":0},"account":{"plan_usage":4}}`, "unknown"},
		{`{"key":{"usage":0,"limit":0},"account":{}}`, "finite"},
	} {
		t.Run(fixture.kind, func(t *testing.T) {
			client := mockClient(t, "tavily", func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != "GET" || request.URL.Path != "/usage" {
					t.Error("usage performed a retrieval")
				}
				writer.Write([]byte(fixture.body))
			})
			items, outcome := client.Usage(t.Context(), "synthetic-only", "account_a", "key_a", "")
			if outcome.Failure != nil || len(items) != 2 || items[0].Used == nil || *items[0].Used != 0 || items[0].LimitKind != fixture.kind {
				t.Fatalf("usage semantics lost: %+v %+v", items, outcome)
			}
			if fixture.kind == "unlimited" && (items[1].Used == nil || *items[1].Used != 6 || items[1].Remaining == nil || *items[1].Remaining != 14) {
				t.Fatal("account totals are not separate")
			}
			if fixture.kind == "unknown" && items[1].Used != nil {
				t.Fatal("missing account usage fabricated")
			}
			for _, item := range items {
				if item.PeriodStart != nil || item.PeriodEnd != nil {
					t.Fatal("invented billing period")
				}
				if err := domain.ValidateJSON("KeyUsage", []byte(store.JSON(item))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestExaUsageRequiresDedicatedOriginAndPreservesPermissionUnknown(t *testing.T) {
	for _, status := range []int{200, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/team-management/api-keys/upstream_1/usage" || request.Header.Get("x-api-key") != "synthetic-service-only" {
					t.Error("wrong usage authentication or identifier")
				}
				writer.WriteHeader(status)
				writer.Write([]byte(`{"id":"upstream_1","api_key_id":"550e8400-e29b-41d4-a716-446655440000","total_cost_usd":0.0000011,"period":{"start":"2026-10-01T00:00:00Z","end":"2026-10-22T00:00:00Z"},"cost_breakdown":[{"cost_usd":99}]}`))
			}))
			defer server.Close()
			address, _ := url.Parse(server.URL)
			client, err := NewExaUsage(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != "admin-api.exa.ai" {
					t.Error("usage origin is not fixed")
				}
				copied := request.Clone(request.Context())
				copied.URL.Scheme, copied.URL.Host, copied.Host = address.Scheme, address.Host, address.Host
				return server.Client().Transport.RoundTrip(copied)
			}))
			if err != nil {
				t.Fatal(err)
			}
			items, _ := client.Usage(t.Context(), "synthetic-service-only", "account_a", "key_a", "upstream_1")
			if len(items) != 1 || items[0].KeyId == nil || items[0].LimitKind != "unknown" {
				t.Fatalf("invented account total/limit: %+v", items)
			}
			if status == 200 && (items[0].Used == nil || *items[0].Used != 2 || items[0].PeriodStart == nil) {
				t.Fatal("cost rounded incorrectly or breakdown counted twice")
			}
			if status == 403 && (items[0].Used != nil || items[0].UnavailableReason == nil || *items[0].UnavailableReason != "service_permission_missing") {
				t.Fatal("permission denial became a free balance")
			}
		})
	}
	ordinary, _ := New("exa", roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("ordinary pooled key used for admin usage")
		return nil, nil
	}))
	if _, outcome := ordinary.Usage(t.Context(), "synthetic-pool-only", "account_a", "key_a", "upstream_1"); outcome.Failure == nil {
		t.Fatal("ordinary provider client accepted team usage")
	}
}
