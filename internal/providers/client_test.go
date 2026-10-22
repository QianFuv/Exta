package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (action roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return action(request)
}

func mockClient(t *testing.T, provider domain.Provider, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	address, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	client, err := New(provider, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		copied := request.Clone(request.Context())
		copied.URL.Scheme = address.Scheme
		copied.URL.Host = address.Host
		copied.Host = address.Host
		return transport.RoundTrip(copied)
	}))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func normalizedInput(operation domain.Operation) Input {
	return Input{Operation: operation, Profile: "common", Query: "synthetic query", MaxResults: 10, ContentMode: "snippet", Urls: []string{"https://example.org/a", "https://example.org/b"}, Url: "https://example.org/", Format: "text", MaxChars: 20000, MaxDepth: 2, MaxBreadth: 20, Limit: 50, ExtractionProfile: "basic"}
}

func TestRetrievalMappingsAndNormalizedResponses(t *testing.T) {
	for _, fixture := range []struct {
		provider            domain.Provider
		operation           domain.Operation
		profile, path, body string
	}{
		{"exa", "search", "common", "/search", `{"requestId":"upstream_1","results":[{"url":"https://example.org/a","title":"Example","highlights":["source text"]}],"costDollars":{"total":0.007}}`},
		{"tavily", "search", "common", "/search", `{"request_id":"upstream_2","results":[{"url":"https://example.org/a","title":"Example","content":"source excerpt","score":0.8}],"usage":{"credits":1},"response_time":0.02}`},
		{"exa", "advanced_search", "exa_deep", "/search", `{"results":[],"output":{"content":"synthesis"},"costDollars":{"total":0.012}}`},
		{"tavily", "advanced_search", "tavily_advanced", "/search", `{"results":[],"answer":"generated answer","usage":{"credits":2}}`},
		{"exa", "fetch", "common", "/contents", `{"results":[{"url":"https://example.org/a","text":"content"}],"statuses":[{"id":"https://example.org/a","status":"success"},{"id":"https://example.org/b","status":"error","error":{"tag":"CRAWL_TIMEOUT","httpStatusCode":504}}],"costDollars":{"total":0.001}}`},
		{"tavily", "fetch", "common", "/extract", `{"results":[{"url":"https://example.org/a","raw_content":"content"}],"failed_results":[{"url":"https://example.org/b","error":"private upstream diagnostic"}],"usage":{"credits":1},"response_time":0.04}`},
		{"tavily", "map", "common", "/map", `{"results":["https://example.org/a"],"usage":{"credits":0},"response_time":0.1}`},
		{"tavily", "crawl", "common", "/crawl", `{"results":[{"url":"https://example.org/a","raw_content":"content"}],"usage":{"credits":2},"response_time":0.1}`},
	} {
		t.Run(string(fixture.provider)+"/"+string(fixture.operation), func(t *testing.T) {
			var calls atomic.Int32
			client := mockClient(t, fixture.provider, func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.Method != "POST" || request.URL.Path != fixture.path {
					t.Errorf("unexpected dispatch %s %s", request.Method, request.URL.Path)
				}
				credential := request.Header.Get("x-api-key")
				if fixture.provider == "tavily" {
					credential = request.Header.Get("Authorization")
				}
				if !strings.Contains(credential, "synthetic-key") {
					t.Error("missing synthetic credential")
				}
				var payload map[string]any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if fixture.provider == "tavily" && payload["include_usage"] != true {
					t.Error("usage not requested")
				}
				if fixture.operation == "search" || fixture.operation == "advanced_search" {
					if fixture.provider == "exa" {
						want := "auto"
						if fixture.profile == "exa_deep" {
							want = "deep"
						}
						if payload["type"] != want {
							t.Errorf("wrong search mode %v", payload["type"])
						}
						if payload["additionalQueries"] != nil {
							t.Error("empty additional query list leaked")
						}
					}
					if fixture.provider == "tavily" {
						want := "basic"
						if fixture.profile == "tavily_advanced" {
							want = "advanced"
						}
						if payload["search_depth"] != want || payload["auto_parameters"] != false || payload["include_domains_mode"] != nil {
							t.Errorf("search defaults changed: %v", payload)
						}
					}
				}
				if fixture.operation == "fetch" && fixture.provider == "exa" {
					if payload["ids"] == nil || payload["urls"] != nil {
						t.Error("canonical contents ids not used")
					}
				}
				if fixture.operation == "map" || fixture.operation == "crawl" {
					if payload["allow_external"] != false || payload["timeout"].(float64) > 150 {
						t.Error("unbounded traversal")
					}
					if payload["select_domains"].([]any)[0] != `^example\.org$` {
						t.Error("hostname not escaped into an exact regex")
					}
				}
				writer.Header().Set("Content-Type", "application/json")
				io.WriteString(writer, fixture.body)
			})
			input := normalizedInput(fixture.operation)
			input.Profile = fixture.profile
			input.SelectDomains = []string{"example.org"}
			if fixture.profile == "exa_deep" {
				input.ProviderOptions = json.RawMessage(`{"kind":"exa_deep","additional_queries":[]}`)
			}
			if fixture.profile == "tavily_advanced" {
				input.ProviderOptions = json.RawMessage(`{"kind":"tavily_advanced","topic":"general"}`)
			}
			outcome := client.Retrieve(t.Context(), "synthetic-key", input, 8<<20)
			if outcome.Failure != nil || calls.Load() != 1 || outcome.Transmission != "response_received" || outcome.Cost == nil {
				t.Fatalf("outcome: %+v calls=%d", outcome, calls.Load())
			}
			if fixture.provider == "exa" && outcome.Certainty != "estimated" {
				t.Error("Exa estimate represented as invoice cost")
			}
			if fixture.operation == "fetch" && (len(outcome.Results) != 1 || len(outcome.Failures) != 1 || outcome.Failures[0].InputIndex != 1 || strings.Contains(outcome.Failures[0].Message, "private")) {
				t.Fatalf("partial URL mapping lost: %+v", outcome)
			}
			for _, item := range outcome.Results {
				if err := domain.ValidateJSON("RetrievalResult", []byte(store.JSON(item))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProviderFailureClassificationDoesNotRetryInternally(t *testing.T) {
	for _, fixture := range []struct {
		provider         domain.Provider
		status           int
		tag, scope, kind string
		retry, unspent   bool
	}{
		{"exa", 401, "", "key", "invalid", true, true}, {"exa", 400, "", "", "", false, true}, {"exa", 403, "FORBIDDEN", "", "", false, true},
		{"exa", 402, "API_KEY_BUDGET_EXCEEDED", "key", "quota", true, true}, {"exa", 402, "TEAM_BUDGET_EXCEEDED", "account", "quota", true, true}, {"exa", 402, "UNKNOWN", "account", "quota", true, true},
		{"exa", 429, "", "account", "rate", true, true}, {"exa", 503, "SERVICE_OVERLOADED", "account", "transient", true, true}, {"exa", 503, "OTHER", "account", "transient", false, false},
		{"tavily", 432, "", "account", "quota", true, true}, {"tavily", 433, "", "account", "quota", true, true}, {"tavily", 402, "", "account", "quota", false, true}, {"tavily", 500, "", "account", "transient", false, false},
	} {
		t.Run(string(fixture.provider)+"/"+fixture.tag+"/"+http.StatusText(fixture.status), func(t *testing.T) {
			var calls atomic.Int32
			client := mockClient(t, fixture.provider, func(writer http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writer.Header().Set("Retry-After", "7")
				writer.WriteHeader(fixture.status)
				json.NewEncoder(writer).Encode(map[string]any{"tag": fixture.tag, "error": "SECRET-UPSTREAM-DIAGNOSTIC"})
			})
			outcome := client.Retrieve(t.Context(), "synthetic-key", normalizedInput("search"), 1024)
			if outcome.Failure == nil || calls.Load() != 1 || outcome.CanRetry != fixture.retry || outcome.IsUnspent != fixture.unspent || outcome.GateScope != fixture.scope || outcome.GateKind != fixture.kind {
				t.Fatalf("classification: %+v", outcome)
			}
			if strings.Contains(outcome.Failure.Error(), "SECRET") {
				t.Fatal("unsafe error disclosure")
			}
			if fixture.status == 429 && outcome.Cooldown != 7*time.Second {
				t.Fatal("Retry-After ignored")
			}
		})
	}
}

func TestMalformedOversizedAndPostTransmissionFailuresRemainUncertain(t *testing.T) {
	for _, body := range []string{`not json`, `{"results":null}`, `{"results":[{"url":"javascript:alert(1)"}]}`, strings.Repeat(" ", 2048)} {
		client := mockClient(t, "exa", func(writer http.ResponseWriter, _ *http.Request) { io.WriteString(writer, body) })
		outcome := client.Retrieve(t.Context(), "synthetic-key", normalizedInput("search"), 1024)
		if outcome.Failure == nil || outcome.IsUnspent || outcome.CanRetry || outcome.Cost != nil {
			t.Fatalf("uncertainty was lost: %+v", outcome)
		}
	}
	for _, sent := range []bool{false, true} {
		client, err := New("exa", roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if sent {
				httptrace.ContextClientTrace(request.Context()).GotConn(httptrace.GotConnInfo{})
			}
			return nil, errors.New("synthetic connection failure")
		}))
		if err != nil {
			t.Fatal(err)
		}
		outcome := client.Retrieve(t.Context(), "synthetic-key", normalizedInput("search"), 1024)
		if outcome.IsUnspent == sent || outcome.CanRetry == sent {
			t.Fatalf("wrong transmission classification: %+v", outcome)
		}
	}
}

func TestRedirectNeverForwardsCredential(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	client := mockClient(t, "exa", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, 307)
	})
	outcome := client.Retrieve(t.Context(), "synthetic-key", normalizedInput("search"), 1024)
	if outcome.Failure == nil || targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestAccountingDecimalAndRetryAfterBoundaries(t *testing.T) {
	for _, fixture := range []struct {
		raw         string
		scale, want int64
		valid       bool
	}{{"0", 1, 0, true}, {"1.2e-3", 1000000, 1200, true}, {"0.0000001", 1000000, 1, true}, {"1.5", 1, 0, false}, {"-1", 1, 0, false}, {"1e999999999", 1, 0, false}, {"9007199254740992", 1, 0, false}, {"null", 1, 0, false}} {
		actual := Amount(json.RawMessage(fixture.raw), fixture.scale)
		if (actual != nil) != fixture.valid || actual != nil && *actual != fixture.want {
			t.Fatalf("%s: %v", fixture.raw, actual)
		}
	}
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if RetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now) != 3*time.Second || RetryAfter("-2", now) != 0 || RetryAfter("999999999999", now) != 24*time.Hour {
		t.Fatal("Retry-After bounds failed")
	}
}

func TestTraversalDeadlineRejectsBeforeTransmission(t *testing.T) {
	var calls atomic.Int32
	client := mockClient(t, "tavily", func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome := client.Retrieve(ctx, "synthetic-key", normalizedInput("crawl"), 1024)
	if outcome.Failure == nil || !outcome.IsUnspent || calls.Load() != 0 {
		t.Fatal("traversal exceeded remaining deadline")
	}
}
