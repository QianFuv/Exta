package providers

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type failedBody struct{}

func (failedBody) Read([]byte) (int, error) { return 0, errors.New("synthetic incomplete response") }
func (failedBody) Close() error             { return nil }

func TestRejectionHeadersSurviveIncompleteAndOversizedBodies(t *testing.T) {
	for _, status := range []int{400, 403, 422, 429} {
		for _, isIncomplete := range []bool{false, true} {
			body := io.ReadCloser(io.NopCloser(strings.NewReader(strings.Repeat("x", 2048))))
			if isIncomplete {
				body = failedBody{}
			}
			client, _ := New("exa", roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"120"}}, Body: body}, nil
			}))
			_, outcome := client.Exchange(t.Context(), "POST", "/search", "synthetic-key-only", map[string]string{"query": "q"}, 1024)
			if outcome.Failure == nil {
				t.Fatal("broken response accepted")
			}
			if status == 429 {
				if !outcome.SharedCooldown || outcome.Cooldown != 2*time.Minute || outcome.GateKind != "rate" {
					t.Fatalf("lost rate headers: %+v", outcome)
				}
			} else if !outcome.RetryProhibited {
				t.Fatalf("lost terminal policy status: %+v", outcome)
			}
		}
	}
}

func TestRetrievalDoesNotReplaceRequestedFormatOrMissingResultEvidence(t *testing.T) {
	client := mockClient(t, "exa", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(`{"results":[],"statuses":[{"id":"https://example.org/a","status":"success"}],"costDollars":{"total":0}}`))
	})
	input := normalizedInput("fetch")
	input.Urls = []string{"https://example.org/a"}
	input.Format = "markdown"
	if result := client.Retrieve(t.Context(), "synthetic-only", input, 1<<20); result.Failure == nil || !result.IsUnspent {
		t.Fatal("unsupported Markdown dispatched")
	}
	input.Format = "text"
	result := client.Retrieve(t.Context(), "synthetic-only", input, 1<<20)
	if len(result.Results) != 0 || len(result.Failures) != 1 || result.Failures[0].Retryable {
		t.Fatalf("status became fabricated result: %+v", result)
	}
	input = normalizedInput("advanced_search")
	input.Profile = "exa_deep"
	input.ProviderOptions = nil
	if _, _, err := client.retrievalPayload(t.Context(), input); err != nil {
		t.Fatalf("optional options rejected: %v", err)
	}
}
