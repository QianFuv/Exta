package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"exta/internal/domain"
	"exta/internal/store"
)

// Retrieve maps one normalized request to one provider exchange, without fallback.
func (client *Client) Retrieve(ctx context.Context, credential string, input Input, limit int64) Outcome {
	path, payload, err := client.retrievalPayload(ctx, input)
	if err != nil {
		return Outcome{Failure: err, IsUnspent: true, Transmission: "not_sent"}
	}
	data, outcome := client.Exchange(ctx, "POST", path, credential, payload, limit)
	if outcome.Failure != nil {
		return outcome
	}
	if outcome.Status != 200 {
		return protocolFailure(outcome)
	}
	return client.normalize(input, data, outcome)
}

func (client *Client) retrievalPayload(ctx context.Context, input Input) (string, map[string]any, *store.Fault) {
	if input.Profile != "common" && !(input.Operation == "advanced_search" && (client.Provider == "exa" && input.Profile == "exa_deep" || client.Provider == "tavily" && input.Profile == "tavily_advanced")) {
		return "", nil, store.Reject("unsupported_capability", 422, "Requested profile cannot be downgraded to this provider")
	}
	payload := map[string]any{}
	path := "/search"
	if client.Provider == "exa" {
		switch input.Operation {
		case "search", "advanced_search":
			payload = map[string]any{"query": input.Query, "type": "auto", "numResults": input.MaxResults}
			if len(input.IncludeDomains) > 0 {
				payload["includeDomains"] = input.IncludeDomains
			}
			if len(input.ExcludeDomains) > 0 {
				payload["excludeDomains"] = input.ExcludeDomains
			}
			contents := map[string]any{"highlights": map[string]any{"maxCharacters": 32000}}
			if input.ContentMode != "snippet" {
				contents["text"] = map[string]any{"maxCharacters": 20000}
			}
			payload["contents"] = contents
			if input.Profile == "exa_deep" {
				payload["type"] = "deep"
				var options domain.ExaDeepOptions
				if json.Unmarshal(input.ProviderOptions, &options) != nil {
					return "", nil, store.Reject("invalid_request", 400, "Invalid Exa options")
				}
				if options.AdditionalQueries != nil && len(*options.AdditionalQueries) > 0 {
					payload["additionalQueries"] = *options.AdditionalQueries
				}
				if options.Category != nil {
					payload["category"] = *options.Category
				}
				if options.PublishedAfter != nil {
					payload["startPublishedDate"] = *options.PublishedAfter
				}
				if options.PublishedBefore != nil {
					payload["endPublishedDate"] = *options.PublishedBefore
				}
				if options.OutputSchema != nil {
					payload["outputSchema"] = *options.OutputSchema
				}
			}
		case "fetch":
			path = "/contents"
			payload = map[string]any{"ids": input.Urls, "text": map[string]any{"maxCharacters": input.MaxChars}}
		default:
			return "", nil, store.Reject("unsupported_capability", 422, "Provider does not support this operation")
		}
		return path, payload, nil
	}
	payload["include_usage"] = true
	switch input.Operation {
	case "search", "advanced_search":
		payload["query"] = input.Query
		payload["max_results"] = input.MaxResults
		payload["search_depth"] = "basic"
		payload["auto_parameters"] = false
		if len(input.IncludeDomains) > 0 {
			payload["include_domains"] = input.IncludeDomains
			payload["include_domains_mode"] = "restrict"
		}
		if len(input.ExcludeDomains) > 0 {
			payload["exclude_domains"] = input.ExcludeDomains
		}
		payload["include_raw_content"] = false
		if input.ContentMode != "snippet" {
			payload["include_raw_content"] = input.ContentMode
		}
		if input.Profile == "tavily_advanced" {
			payload["search_depth"] = "advanced"
			var options map[string]json.RawMessage
			if json.Unmarshal(input.ProviderOptions, &options) != nil {
				return "", nil, store.Reject("invalid_request", 400, "Invalid Tavily options")
			}
			for _, key := range []string{"topic", "start_date", "end_date", "country", "include_answer"} {
				if value, exists := options[key]; exists {
					payload[key] = value
				}
			}
		}
	case "fetch":
		path = "/extract"
		payload["urls"] = input.Urls
		payload["format"] = input.Format
		payload["extract_depth"] = "basic"
	case "map", "crawl":
		path = "/" + string(input.Operation)
		payload["url"] = input.Url
		payload["max_depth"] = input.MaxDepth
		payload["max_breadth"] = input.MaxBreadth
		payload["limit"] = input.Limit
		payload["allow_external"] = false
		if len(input.SelectPaths) > 0 {
			payload["select_paths"] = input.SelectPaths
		}
		if len(input.ExcludePaths) > 0 {
			payload["exclude_paths"] = input.ExcludePaths
		}
		for key, hosts := range map[string][]string{"select_domains": input.SelectDomains, "exclude_domains": input.ExcludeDomains} {
			if len(hosts) > 0 {
				patterns := make([]string, len(hosts))
				for index, host := range hosts {
					patterns[index] = "^" + regexp.QuoteMeta(host) + "$"
				}
				payload[key] = patterns
			}
		}
		if input.Instructions != "" {
			payload["instructions"] = input.Instructions
		}
		timeout := int64(150)
		if deadline, exists := ctx.Deadline(); exists {
			timeout = min(timeout, int64(time.Until(deadline)/time.Second)-1)
		}
		if timeout < 10 {
			return "", nil, store.Reject("deadline_exceeded", 504, "Remaining deadline is below the provider traversal minimum")
		}
		payload["timeout"] = timeout
		if input.Operation == "crawl" {
			payload["format"] = input.Format
			payload["extract_depth"] = input.ExtractionProfile
		}
	default:
		return "", nil, store.Reject("unsupported_capability", 422, "Provider does not support this operation")
	}
	return path, payload, nil
}

type retrievalEnvelope struct {
	Results json.RawMessage `json:"results"`
	Output  json.RawMessage `json:"output"`
	Answer  json.RawMessage `json:"answer"`
	Cost    struct {
		Total json.RawMessage `json:"total"`
	} `json:"costDollars"`
	Usage struct {
		Credits json.RawMessage `json:"credits"`
	} `json:"usage"`
	Statuses []struct {
		Id     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Tag    string `json:"tag"`
			Status *int   `json:"httpStatusCode"`
		} `json:"error"`
	} `json:"statuses"`
	Failed []struct {
		Url   string `json:"url"`
		Error string `json:"error"`
	} `json:"failed_results"`
}

type retrievalItem struct {
	Url           string          `json:"url"`
	Title         *string         `json:"title"`
	Text          *string         `json:"text"`
	Content       *string         `json:"content"`
	RawContent    *string         `json:"raw_content"`
	Highlights    []string        `json:"highlights"`
	Published     *string         `json:"publishedDate"`
	PublishedDate *string         `json:"published_date"`
	Score         json.RawMessage `json:"score"`
}

func protocolFailure(outcome Outcome) Outcome {
	outcome.Failure = store.Reject("upstream_protocol_error", 502, "Provider response does not match the supported protocol")
	outcome.Cost = nil
	outcome.Certainty = "unknown"
	outcome.CanRetry = false
	outcome.Results = []domain.RetrievalResult{}
	outcome.Failures = []domain.ItemFailure{}
	return outcome
}

func nullableText(value *string, limit int) (*string, bool) {
	if value == nil {
		return nil, false
	}
	if utf8.RuneCountInString(*value) <= limit {
		return value, false
	}
	clipped := string([]rune(*value)[:limit])
	return &clipped, true
}

func responseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 4096 && parsed.User == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") && !strings.Contains(value, "\\")
}

func (client *Client) normalize(input Input, data []byte, outcome Outcome) Outcome {
	var envelope retrievalEnvelope
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Results) == 0 || envelope.Results[0] != '[' {
		return protocolFailure(outcome)
	}
	if client.Provider == "exa" {
		outcome.Cost = Amount(envelope.Cost.Total, 1000000)
		outcome.Certainty = "estimated"
	} else {
		outcome.Cost = Amount(envelope.Usage.Credits, 1)
		outcome.Certainty = "reported"
	}
	if outcome.Cost == nil {
		outcome.Certainty = "unknown"
	}
	items := []retrievalItem{}
	if input.Operation == "map" {
		var links []string
		if json.Unmarshal(envelope.Results, &links) != nil {
			return protocolFailure(outcome)
		}
		for _, link := range links {
			items = append(items, retrievalItem{Url: link})
		}
	} else if json.Unmarshal(envelope.Results, &items) != nil {
		return protocolFailure(outcome)
	}
	maximum := int(input.MaxResults)
	if input.Operation == "fetch" {
		maximum = len(input.Urls)
	} else if input.Operation == "map" || input.Operation == "crawl" {
		maximum = int(input.Limit)
	}
	if len(items) > maximum {
		return protocolFailure(outcome)
	}
	for _, item := range items {
		if !responseURL(item.Url) {
			return protocolFailure(outcome)
		}
		metadata := map[string]any{}
		result := domain.RetrievalResult{Url: item.Url, SourceProvider: client.Provider, ProviderMetadata: json.RawMessage(`{}`)}
		result.Title, _ = nullableText(item.Title, 2048)
		if client.Provider == "exa" {
			if len(item.Highlights) > 0 {
				snippet := strings.Join(item.Highlights, "\n")
				result.Snippet = &snippet
			}
			result.Content = item.Text
			result.PublishedAt = item.Published
		} else {
			result.Snippet = item.Content
			result.Content = item.RawContent
			result.PublishedAt = item.PublishedDate
			if len(item.Score) > 0 {
				var score json.Number
				if json.Unmarshal(item.Score, &score) == nil {
					metadata["score"] = score
				}
			}
		}
		result.Snippet, _ = nullableText(result.Snippet, 32000)
		if result.Snippet != nil {
			kind := "source_excerpt"
			result.SnippetKind = &kind
		}
		if result.PublishedAt != nil {
			if _, err := time.Parse(time.RFC3339Nano, *result.PublishedAt); err != nil {
				original, _ := nullableText(result.PublishedAt, 128)
				metadata["original_published_at"] = original
				result.PublishedAt = nil
			}
		}
		contentLimit := 20000
		if input.Operation == "fetch" {
			contentLimit = int(input.MaxChars)
		}
		if input.Operation == "crawl" {
			contentLimit = 1000000
		}
		var clipped bool
		result.Content, clipped = nullableText(result.Content, contentLimit)
		if clipped {
			metadata["content_truncated"] = true
			outcome.Warnings = append(outcome.Warnings, "Provider content was truncated to the requested character limit")
		}
		if result.Content != nil {
			format := input.Format
			if input.Operation == "search" || input.Operation == "advanced_search" {
				format = input.ContentMode
			}
			if client.Provider == "exa" || format == "snippet" {
				format = "text"
			}
			result.ContentFormat = &format
		}
		result.ProviderMetadata = json.RawMessage(store.JSON(metadata))
		outcome.Results = append(outcome.Results, result)
	}
	if input.Operation == "fetch" {
		failed := map[string]domain.ItemFailure{}
		for _, status := range envelope.Statuses {
			if status.Status == "error" {
				code := "fetch_failed"
				canRetry := false
				if status.Error != nil && safeIdentifier.MatchString(status.Error.Tag) {
					code = status.Error.Tag
					canRetry = code == "CRAWL_TIMEOUT" || code == "CRAWL_FAILED"
				}
				failed[status.Id] = domain.ItemFailure{Url: status.Id, Code: code, Message: "Provider could not retrieve this URL", Retryable: canRetry, Provider: client.Provider}
			}
		}
		for _, failure := range envelope.Failed {
			failed[failure.Url] = domain.ItemFailure{Url: failure.Url, Code: "fetch_failed", Message: "Provider could not retrieve this URL", Retryable: false, Provider: client.Provider}
		}
		for index, link := range input.Urls {
			if failure, exists := failed[link]; exists {
				failure.InputIndex = domain.Integer(index)
				outcome.Failures = append(outcome.Failures, failure)
				continue
			}
			found := false
			for _, result := range outcome.Results {
				if result.Url == link {
					found = true
					break
				}
			}
			if !found {
				for _, status := range envelope.Statuses {
					if status.Id == link && status.Status == "success" {
						found = true
						break
					}
				}
			}
			if !found {
				outcome.Failures = append(outcome.Failures, domain.ItemFailure{Url: link, InputIndex: domain.Integer(index), Code: "result_missing", Message: "Provider omitted a result for this URL", Provider: client.Provider})
			}
		}
	}
	output := map[string]any{}
	if client.Provider == "exa" && len(envelope.Output) > 0 && string(envelope.Output) != "null" {
		output["exa"] = map[string]any{"output": envelope.Output}
	}
	if client.Provider == "tavily" && len(envelope.Answer) > 0 && string(envelope.Answer) != "null" {
		output["tavily"] = map[string]any{"answer": envelope.Answer}
	}
	if len(output) > 0 {
		value := json.RawMessage(store.JSON(output))
		outcome.Output = &value
	}
	if client.Provider == "exa" && (input.ContentMode == "markdown" || input.Format == "markdown") {
		outcome.Warnings = append(outcome.Warnings, "Exa extraction is returned as text without a Markdown fidelity guarantee")
	}
	return outcome
}
