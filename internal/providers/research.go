package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

var researchIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,200}$`)

func researchPath(id string) string {
	if id == "." || id == ".." {
		return strings.ReplaceAll(id, ".", "%2E")
	}
	return url.PathEscape(id)
}

// ResearchOutcome keeps verified identity and terminal evidence even when report parsing fails.
type ResearchOutcome struct {
	Outcome
	ID, State, StopReason string
	Report                *domain.ResearchReport
	Request               json.RawMessage
	CreatedAt             time.Time
}

// ResearchListFailure preserves rate and credential evidence from failed discovery calls.
type ResearchListFailure struct{ Outcome Outcome }

func (failure *ResearchListFailure) Error() string { return failure.Outcome.Failure.Error() }

// ResearchPayload preserves explicit budget and profile semantics without provider defaults.
func ResearchPayload(provider domain.Provider, input domain.ResearchCreate, jobID, digest string) (map[string]any, error) {
	if input.Profile == nil {
		return nil, store.Reject("invalid_request", 400, "Research profile must be resolved")
	}
	var budget domain.HardBudget
	if err := json.Unmarshal(input.Budget, &budget); err != nil {
		return nil, store.Reject("invalid_request", 400, "Invalid research budget")
	}
	payload := map[string]any{}
	if provider == "exa" && *input.Profile == "exa_auto" && budget.Mode == "hard" && budget.Unit == "micro_usd" {
		payload = map[string]any{"query": input.Input, "effort": "auto", "budget": map[string]any{"maxCostDollars": json.Number(strconv.FormatInt(int64(budget.Amount)/1000000, 10) + "." + leftPad(int64(budget.Amount)%1000000))}, "metadata": map[string]string{"exta_job_id": jobID, "exta_intent_digest": digest}}
		if input.OutputSchema != nil {
			payload["outputSchema"] = *input.OutputSchema
		}
	} else if provider == "tavily" && (*input.Profile == "tavily_mini" || *input.Profile == "tavily_pro") && budget.Mode == "estimated" && budget.Unit == "tavily_credit" {
		payload = map[string]any{"input": input.Input, "model": strings.TrimPrefix(*input.Profile, "tavily_"), "stream": false}
		if input.OutputSchema != nil {
			payload["output_schema"] = *input.OutputSchema
		}
	} else {
		return nil, store.Reject("budget_guarantee_unavailable", 422, "Research profile and budget guarantee are incompatible")
	}
	return payload, nil
}

func leftPad(value int64) string {
	text := strconv.FormatInt(value, 10)
	return strings.Repeat("0", 6-len(text)) + text
}

// ResearchCreate performs one create attempt. The caller must persist intent first.
func (client *Client) ResearchCreate(ctx context.Context, credential string, input domain.ResearchCreate, jobID, digest string, limit int64) ResearchOutcome {
	payload, err := ResearchPayload(client.Provider, input, jobID, digest)
	if err != nil {
		return ResearchOutcome{Outcome: Outcome{Failure: err.(*store.Fault), IsUnspent: true, Transmission: "not_sent"}}
	}
	path := "/agent/runs"
	if client.Provider == "tavily" {
		path = "/research"
	}
	data, outcome := client.Exchange(ctx, http.MethodPost, path, credential, payload, limit)
	if outcome.Failure != nil {
		return ResearchOutcome{Outcome: outcome}
	}
	return client.researchResult(data, outcome, "")
}

// ResearchGet observes the original task with its pinned credential; it never creates work.
func (client *Client) ResearchGet(ctx context.Context, credential, id string, limit int64) ResearchOutcome {
	if !researchIdentifier.MatchString(id) {
		return ResearchOutcome{Outcome: Outcome{Failure: store.Reject("upstream_protocol_error", 502, "Unsupported provider task identifier")}}
	}
	path := "/agent/runs/" + researchPath(id)
	if client.Provider == "tavily" {
		path = "/research/" + researchPath(id) + "?include_usage=true"
	}
	data, outcome := client.Exchange(ctx, http.MethodGet, path, credential, nil, limit)
	if outcome.Failure != nil {
		return ResearchOutcome{Outcome: outcome}
	}
	return client.researchResult(data, outcome, id)
}

// ResearchCancel requests Exa cancellation and preserves a racing completion response.
func (client *Client) ResearchCancel(ctx context.Context, credential, id string, limit int64) ResearchOutcome {
	if client.Provider != "exa" {
		return ResearchOutcome{Outcome: Outcome{Failure: store.Reject("cancel_not_supported", 409, "Provider does not support upstream cancellation")}}
	}
	if !researchIdentifier.MatchString(id) {
		return ResearchOutcome{Outcome: Outcome{Failure: store.Reject("upstream_protocol_error", 502, "Unsupported provider task identifier")}}
	}
	data, outcome := client.Exchange(ctx, http.MethodPost, "/agent/runs/"+researchPath(id)+"/cancel", credential, nil, limit)
	if outcome.Failure != nil {
		return ResearchOutcome{Outcome: outcome}
	}
	return client.researchResult(data, outcome, id)
}

// ResearchList reads one bounded Exa reconciliation page; absence never proves non-submission.
func (client *Client) ResearchList(ctx context.Context, credential, cursor string, limit int64) ([]ResearchOutcome, string, bool, error) {
	if client.Provider != "exa" || len(cursor) > 2048 {
		return nil, "", false, store.Reject("unsupported_capability", 422, "Provider does not offer bounded research listing")
	}
	path := "/agent/runs?limit=100"
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	data, outcome := client.Exchange(ctx, http.MethodGet, path, credential, nil, limit)
	if outcome.Failure != nil {
		return nil, "", false, &ResearchListFailure{Outcome: outcome}
	}
	var page struct {
		Data       []json.RawMessage `json:"data"`
		HasMore    *bool             `json:"hasMore"`
		NextCursor *string           `json:"nextCursor"`
	}
	if json.Unmarshal(data, &page) != nil || page.Data == nil || page.HasMore == nil || len(page.Data) > 100 {
		return nil, "", false, store.Reject("upstream_protocol_error", 502, "Invalid research listing")
	}
	items := []ResearchOutcome{}
	for _, raw := range page.Data {
		item := client.researchResult(raw, outcome, "")
		if item.ID == "" {
			return nil, "", false, store.Reject("upstream_protocol_error", 502, "Incomplete research listing identity")
		}
		items = append(items, item)
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	if *page.HasMore && (next == "" || len(next) > 2048) {
		return nil, "", false, store.Reject("upstream_protocol_error", 502, "Incomplete research listing cursor")
	}
	return items, next, *page.HasMore, nil
}

func (client *Client) researchResult(data []byte, outcome Outcome, expectedID string) ResearchOutcome {
	result := ResearchOutcome{Outcome: outcome}
	var body struct {
		ID         string          `json:"id"`
		RequestID  string          `json:"request_id"`
		Status     string          `json:"status"`
		StopReason string          `json:"stopReason"`
		CreatedAt  string          `json:"createdAt"`
		Request    json.RawMessage `json:"request"`
		Output     json.RawMessage `json:"output"`
		Content    json.RawMessage `json:"content"`
		Sources    []struct {
			Title *string `json:"title"`
			URL   string  `json:"url"`
		} `json:"sources"`
		Cost struct {
			Total json.RawMessage `json:"total"`
		} `json:"costDollars"`
		Usage struct {
			Credits json.RawMessage `json:"credits"`
		} `json:"usage"`
	}
	fail := func() ResearchOutcome {
		result.Failure = store.Reject("upstream_protocol_error", 502, "Research response does not match the supported contract")
		return result
	}
	if json.Unmarshal(data, &body) != nil {
		return fail()
	}
	id := body.ID
	if client.Provider == "tavily" {
		id = body.RequestID
	}
	if !researchIdentifier.MatchString(id) || expectedID != "" && id != expectedID {
		return fail()
	}
	result.ID = id
	result.State = body.Status
	result.StopReason = body.StopReason
	result.Request = body.Request
	result.CreatedAt, _ = time.Parse(time.RFC3339Nano, body.CreatedAt)
	if client.Provider == "exa" {
		result.Cost = Amount(body.Cost.Total, 1000000)
	} else {
		result.Cost = Amount(body.Usage.Credits, 1)
	}
	if result.Cost != nil {
		result.Certainty = "reported"
	}
	if len(result.State) == 0 || len(result.State) > 128 || len(result.StopReason) > 128 {
		if len(result.State) > 128 {
			result.State = ""
		}
		if len(result.StopReason) > 128 {
			result.StopReason = ""
		}
		return fail()
	}
	if result.State != "completed" {
		return result
	}
	report := domain.ResearchReport{Citations: []domain.Citation{}}
	addCitation := func(address string, title *string) bool {
		normalized, err := security.NormalizeURL(address)
		if err != nil || len(report.Citations) >= 500 {
			return false
		}
		report.Citations = append(report.Citations, domain.Citation{Id: "citation_" + strconv.Itoa(len(report.Citations)+1), Url: normalized, Title: title, SourceProvider: client.Provider})
		return true
	}
	structured := json.RawMessage(nil)
	if client.Provider == "exa" {
		var output struct {
			Text       *string         `json:"text"`
			Structured json.RawMessage `json:"structured"`
			Grounding  []struct {
				Citations []struct {
					URL   string  `json:"url"`
					Title *string `json:"title"`
				} `json:"citations"`
			} `json:"grounding"`
		}
		if len(body.Output) == 0 || json.Unmarshal(body.Output, &output) != nil {
			return fail()
		}
		report.Markdown = output.Text
		structured = output.Structured
		for _, grounding := range output.Grounding {
			for _, citation := range grounding.Citations {
				if !addCitation(citation.URL, citation.Title) {
					return fail()
				}
			}
		}
	} else {
		if len(body.Content) == 0 {
			return fail()
		}
		var markdown string
		if json.Unmarshal(body.Content, &markdown) == nil {
			report.Markdown = &markdown
		} else {
			structured = body.Content
		}
		for _, source := range body.Sources {
			if !addCitation(source.URL, source.Title) {
				return fail()
			}
		}
	}
	if len(structured) > 0 && string(structured) != "null" {
		var object map[string]json.RawMessage
		if json.Unmarshal(structured, &object) != nil || object == nil {
			return fail()
		}
		report.StructuredOutput = &structured
	}
	if report.Markdown == nil && report.StructuredOutput == nil {
		return fail()
	}
	if domain.ValidateJSON("ResearchReport", []byte(store.JSON(report))) != nil {
		return fail()
	}
	result.Report = &report
	return result
}
