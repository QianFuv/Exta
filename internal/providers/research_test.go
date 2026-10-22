package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"exta/internal/domain"
)

func TestResearchIdentifierIsOneOpaquePathSegment(t *testing.T) {
	for _, id := range []string{".task:with-punctuation_1", strings.Repeat("a", 200), ".", ".."} {
		client, err := New("exa", roundTripFunc(func(request *http.Request) (*http.Response, error) {
			expected := "/agent/runs/" + id
			if id == "." || id == ".." {
				expected = "/agent/runs/" + strings.ReplaceAll(id, ".", "%2E")
			}
			if request.URL.EscapedPath() != expected {
				t.Fatalf("ID changed path: %s", request.URL.EscapedPath())
			}
			data, _ := json.Marshal(map[string]any{"id": id, "status": "running"})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		outcome := client.ResearchGet(t.Context(), "synthetic-key", id, 1<<20)
		if outcome.ID != id || outcome.Failure != nil {
			t.Fatalf("opaque ID rejected: %+v", outcome)
		}
	}
}

func TestResearchListingRequiresExplicitPaginationCompleteness(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"data":[],"hasMore":null}`, `{"data":[],"hasMore":true}`} {
		client := mockClient(t, "exa", func(writer http.ResponseWriter, _ *http.Request) { writer.Write([]byte(body)) })
		if _, _, _, err := client.ResearchList(t.Context(), "synthetic-key", "", 1<<20); err == nil {
			t.Fatalf("accepted incomplete list %s", body)
		}
	}
}

func TestResearchExaExplicitCapAndPartialCancellationRace(t *testing.T) {
	calls := 0
	client := mockClient(t, "exa", func(writer http.ResponseWriter, request *http.Request) {
		calls++
		body, _ := io.ReadAll(request.Body)
		switch calls {
		case 1:
			var payload map[string]json.RawMessage
			if request.URL.Path != "/agent/runs" || request.Method != "POST" || json.Unmarshal(body, &payload) != nil {
				t.Fatal("invalid create wire")
			}
			if string(payload["effort"]) != `"auto"` || string(payload["budget"]) != `{"maxCostDollars":1.000001}` {
				t.Fatalf("budget defaulted: %s", body)
			}
			writer.Write([]byte(`{"id":"agent_run_fixture","status":"queued"}`))
		case 2:
			if request.URL.Path != "/agent/runs/agent_run_fixture/cancel" || len(body) != 0 {
				t.Fatalf("cancel body/path: %s %s", request.URL.Path, body)
			}
			writer.Write([]byte(`{"id":"agent_run_fixture","status":"completed","stopReason":"budget_reached","output":{"text":"partial report","structured":null,"grounding":[{"citations":[{"url":"https://example.com/a"}]}]},"costDollars":{"total":0.25}}`))
		default:
			t.Fatal("unexpected extra call")
		}
	})
	profile := "exa_auto"
	input := domain.ResearchCreate{Input: "synthetic input", Profile: &profile, Budget: json.RawMessage(`{"mode":"hard","unit":"micro_usd","amount":1000001}`)}
	created := client.ResearchCreate(t.Context(), "synthetic-key", input, "job_local", "digest", 1<<20)
	if created.Failure != nil || created.ID != "agent_run_fixture" || created.State != "queued" {
		t.Fatalf("create: %+v", created)
	}
	completed := client.ResearchCancel(t.Context(), "synthetic-key", created.ID, 1<<20)
	if completed.Failure != nil || completed.State != "completed" || completed.StopReason != "budget_reached" || completed.Report == nil || completed.Cost == nil || *completed.Cost != 250000 {
		t.Fatalf("completion race lost: %+v", completed)
	}
}

func TestResearchTavilyCreateAndFailedHTTP200(t *testing.T) {
	calls := 0
	client := mockClient(t, "tavily", func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if calls == 1 {
			var payload map[string]any
			_ = json.NewDecoder(request.Body).Decode(&payload)
			if payload["model"] != "mini" || payload["stream"] != false || payload["input"] != "synthetic input" {
				t.Fatal("Tavily implicit model")
			}
			writer.WriteHeader(201)
			writer.Write([]byte(`{"request_id":"task_fixture","status":"pending","response_time":0.23}`))
		} else {
			if request.Method != "GET" || request.URL.RequestURI() != "/research/task_fixture?include_usage=true" {
				t.Fatal("wrong observation path")
			}
			writer.Write([]byte(`{"request_id":"task_fixture","status":"failed","usage":{"credits":0},"response_time":0.17}`))
		}
	})
	profile := "tavily_mini"
	input := domain.ResearchCreate{Input: "synthetic input", Profile: &profile, Budget: json.RawMessage(`{"mode":"estimated","unit":"tavily_credit","amount":110,"acknowledge_estimated_budget":true}`)}
	created := client.ResearchCreate(t.Context(), "synthetic-key", input, "job_local", "digest", 1<<20)
	if created.ID != "task_fixture" || created.Failure != nil {
		t.Fatalf("create: %+v", created)
	}
	failed := client.ResearchGet(t.Context(), "synthetic-key", created.ID, 1<<20)
	if failed.State != "failed" || failed.Failure != nil || failed.Cost == nil || *failed.Cost != 0 {
		t.Fatalf("HTTP 200 failed lost: %+v", failed)
	}
	if result := client.ResearchCancel(t.Context(), "synthetic-key", created.ID, 1<<20); result.Failure == nil || calls != 2 {
		t.Fatal("Tavily cancellation invented")
	}
}

func TestResearchUnknownOrMalformedResultPreservesIdentityAndCost(t *testing.T) {
	for _, fixture := range []struct {
		body, state string
		failure     bool
	}{{`{"id":"task.with:punctuation","status":"future_status"}`, "future_status", false}, {`{"id":"task_fixture","status":"completed","output":{"structured":[1,2]},"costDollars":{"total":1}}`, "completed", true}} {
		t.Run(fixture.state, func(t *testing.T) {
			client := mockClient(t, "exa", func(writer http.ResponseWriter, _ *http.Request) { writer.Write([]byte(fixture.body)) })
			result := client.ResearchGet(t.Context(), "synthetic-key", map[bool]string{true: "task_fixture", false: "task.with:punctuation"}[fixture.failure], 1<<20)
			if result.ID == "" || result.State != fixture.state || (result.Failure != nil) != fixture.failure {
				t.Fatalf("lost evidence: %+v", result)
			}
		})
	}
}

func TestResearchAgentTimeoutNeverProvesNonSubmission(t *testing.T) {
	client := mockClient(t, "exa", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(504)
		writer.Write([]byte(`{"error":{"type":"SERVER_ERROR","code":"TIMEOUT","message":"synthetic"}}`))
	})
	profile := "exa_auto"
	result := client.ResearchCreate(t.Context(), "synthetic-key", domain.ResearchCreate{Input: "q", Profile: &profile, Budget: json.RawMessage(`{"mode":"hard","unit":"micro_usd","amount":1000000}`)}, "job_local", "digest", 1<<20)
	if result.Failure == nil || result.IsUnspent || result.CanRetry || result.Failure.Details["upstream_tag"] != "TIMEOUT" {
		t.Fatalf("unsafe timeout: %+v", result)
	}
}
