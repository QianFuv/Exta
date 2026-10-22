// Package service owns shared semantic validation, admission and provider orchestration.
package service

import (
	"context"
	_ "embed"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/store"
)

//go:embed tavily-countries.json
var tavilyCountries []byte

// SchemaName maps public operation names to the frozen request boundary.
func SchemaName(operation string) string {
	return map[string]string{"search": "SearchRequest", "advanced_search": "AdvancedSearchRequest", "fetch": "FetchRequest", "map": "MapRequest", "crawl": "CrawlRequest", "research_create": "ResearchCreate"}[operation]
}

// Prepare validates input before admission. Preview never resolves DNS or touches providers.
func Prepare(ctx context.Context, policy security.UrlPolicy, operation string, raw []byte, preview bool) (providers.Input, error) {
	input := providers.Input{Operation: domain.Operation(operation), Profile: "common", MaxResults: 10, ContentMode: "snippet", Format: "text", MaxChars: 20000, MaxDepth: 2, MaxBreadth: 20, Limit: 50, ExtractionProfile: "basic"}
	if operation == "crawl" {
		input.Format = "markdown"
	}
	schema := SchemaName(operation)
	if schema == "" || operation == "research_create" {
		return input, store.Reject("unsupported_capability", 422, "Unsupported retrieval operation")
	}
	if err := domain.ValidateJSON(schema, raw); err != nil {
		return input, store.Reject("invalid_request", 400, "Request does not match the operation schema")
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, store.Reject("invalid_request", 400, "Request cannot be decoded")
	}
	if len(input.ProviderOptions) == 0 {
		input.ProviderOptions = json.RawMessage(`{}`)
	}
	for _, list := range []*[]string{&input.IncludeDomains, &input.ExcludeDomains, &input.SelectDomains} {
		for index, hostname := range *list {
			canonical, err := security.Hostname(hostname)
			if err != nil {
				return input, err
			}
			(*list)[index] = canonical
		}
	}
	for _, pattern := range append(append([]string{}, input.SelectPaths...), input.ExcludePaths...) {
		if _, err := regexp.Compile(pattern); err != nil {
			return input, parameterError("select_paths/exclude_paths", "Path patterns must use the supported regular-expression subset")
		}
	}
	if input.Profile == "exa_deep" {
		var options domain.ExaDeepOptions
		if err := json.Unmarshal(input.ProviderOptions, &options); err != nil {
			return input, parameterError("provider_options", "Exa deep options are required")
		}
		if options.Category != nil && (*options.Category == "company" || *options.Category == "people") && (options.PublishedAfter != nil || options.PublishedBefore != nil) {
			return input, parameterError("provider_options", "This category does not support publication filters")
		}
		if options.Category != nil && *options.Category == "people" && len(input.ExcludeDomains) > 0 {
			return input, parameterError("exclude_domains", "People search does not support excluded domains")
		}
		if options.PublishedAfter != nil && options.PublishedBefore != nil {
			start, _ := time.Parse(time.RFC3339Nano, *options.PublishedAfter)
			end, _ := time.Parse(time.RFC3339Nano, *options.PublishedBefore)
			if start.After(end) {
				return input, parameterError("provider_options", "Publication interval is reversed")
			}
		}
	}
	if input.Profile == "tavily_advanced" {
		var options domain.TavilyAdvancedOptions
		if err := json.Unmarshal(input.ProviderOptions, &options); err != nil {
			return input, parameterError("provider_options", "Tavily advanced options are required")
		}
		if options.Country != nil {
			var countries []string
			if json.Unmarshal(tavilyCountries, &countries) != nil {
				return input, store.Reject("storage_unavailable", 503, "Country capability data is unavailable")
			}
			if !slices.Contains(countries, *options.Country) || options.Topic != nil && *options.Topic != "general" {
				return input, parameterError("provider_options.country", "Country must be an audited Tavily country for the general topic")
			}
		}
		if options.StartDate != nil && options.EndDate != nil && *options.StartDate > *options.EndDate {
			return input, parameterError("provider_options", "Date interval is reversed")
		}
	}
	validateURL := func(value string) (string, error) {
		normalized, err := security.NormalizeURL(value)
		if err != nil {
			return "", err
		}
		if utf8.RuneCountInString(normalized) > 4096 {
			return "", parameterError("url", "Normalized URL exceeds the wire limit")
		}
		if preview {
			return normalized, nil
		}
		return policy.Validate(ctx, normalized)
	}
	var err error
	if operation == "fetch" {
		for index, value := range input.Urls {
			input.Urls[index], err = validateURL(value)
			if err != nil {
				return input, err
			}
		}
	}
	if operation == "map" || operation == "crawl" {
		input.Url, err = validateURL(input.Url)
		if err != nil {
			return input, err
		}
	}
	return input, nil
}

func parameterError(field, message string) *store.Fault {
	fault := store.Reject("unsupported_parameter", 422, message)
	fault.Details["field"] = field
	return fault
}

// SupportedTarget rejects policy combinations which no adapter can execute faithfully.
func SupportedTarget(operation string, target domain.RouteTarget) bool {
	switch operation {
	case "search", "fetch":
		return (target.Provider == "exa" || target.Provider == "tavily") && target.Profile == "common"
	case "advanced_search":
		return target.Provider == "exa" && (target.Profile == "common" || target.Profile == "exa_deep") || target.Provider == "tavily" && (target.Profile == "common" || target.Profile == "tavily_advanced")
	case "map", "crawl":
		return target.Provider == "tavily" && target.Profile == "common"
	case "research_create":
		return target.Provider == "exa" && target.Profile == "exa_auto" || target.Provider == "tavily" && (target.Profile == "tavily_mini" || target.Profile == "tavily_pro")
	}
	return false
}

// ValidateRoute prevents invalid profiles and expanded hard limits from entering configuration.
func ValidateRoute(operation string, input domain.RouteReplace) error {
	if SchemaName(operation) == "" {
		return store.Reject("not_found", 404, "Unknown capability")
	}
	if err := domain.ValidateJSON("RouteReplace", []byte(store.JSON(input))); err != nil {
		return store.Reject("invalid_request", 400, "Route does not match the operation schema")
	}
	maximum := int64(60)
	if operation == "search" || operation == "research_create" {
		maximum = 30
	}
	if operation == "map" || operation == "crawl" {
		maximum = 165
	}
	if int64(input.DeadlineSeconds) > maximum {
		return parameterError("deadline_seconds", "Route deadline exceeds this capability's hard limit")
	}
	if operation == "research_create" && input.AllowDuplicateCost {
		return parameterError("allow_duplicate_cost", "Research cannot enable uncertain create retries")
	}
	for _, target := range input.Targets {
		if !SupportedTarget(operation, target) {
			return parameterError("targets", "Each target must preserve a supported provider profile")
		}
	}
	return nil
}

// Compatible preserves explicit profile semantics and provider wire limits.
func Compatible(input providers.Input, target domain.RouteTarget) bool {
	if !SupportedTarget(string(input.Operation), target) || string(target.Profile) != input.Profile {
		return false
	}
	if target.Provider == "exa" && (input.ContentMode == "markdown" || input.Operation == "fetch" && input.Format == "markdown") {
		return false
	}
	if input.Operation == "fetch" && target.Provider == "exa" {
		for _, link := range input.Urls {
			if utf8.RuneCountInString(link) > 2048 {
				return false
			}
		}
	}
	return true
}

// Preview returns configuration eligibility without tokens, holds, key validation or upstream calls.
func (service *Service) Preview(ctx context.Context, raw []byte) (domain.RoutePreview, error) {
	result := domain.RoutePreview{Eligible: []domain.RouteTarget{}, Exclusions: []json.RawMessage{}, Warnings: []string{}}
	if err := domain.ValidateJSON("RoutePreviewRequest", raw); err != nil {
		return result, store.Reject("invalid_request", 400, "Invalid route preview request")
	}
	var request struct {
		Operation string          `json:"operation"`
		Request   json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return result, store.Reject("invalid_json", 400, "Invalid preview JSON")
	}
	route, err := service.Store.Route(ctx, request.Operation)
	if err != nil {
		return result, err
	}
	result.RouteRevision = route.Revision
	profile := ""
	var input providers.Input
	if request.Operation == "research_create" {
		var research domain.ResearchCreate
		if err = json.Unmarshal(request.Request, &research); err != nil {
			return result, err
		}
		profile = "exa_auto"
		if research.Profile != nil {
			profile = *research.Profile
		} else if len(route.Targets) > 0 {
			profile = string(route.Targets[0].Profile)
		}
		var budget struct {
			Unit string `json:"unit"`
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(research.Budget, &budget)
		if strings.HasPrefix(profile, "exa_") && budget.Unit != "micro_usd" || strings.HasPrefix(profile, "tavily_") && budget.Mode != "estimated" {
			return result, store.Reject("budget_guarantee_unavailable", 422, "Budget guarantee does not match the selected research profile")
		}
	} else {
		input, err = Prepare(ctx, service.URLs, request.Operation, request.Request, true)
		if err != nil {
			return result, err
		}
		profile = input.Profile
	}
	for _, target := range route.Targets {
		isCompatible := string(target.Profile) == profile && SupportedTarget(request.Operation, target)
		if request.Operation != "research_create" {
			isCompatible = Compatible(input, target)
		}
		if route.Enabled && isCompatible {
			result.Eligible = append(result.Eligible, target)
		} else {
			result.Exclusions = append(result.Exclusions, json.RawMessage(store.JSON(map[string]any{"provider": target.Provider, "field": "profile", "reason": "Route is disabled or cannot preserve the requested profile and wire limits"})))
		}
	}
	result.Warnings = append(result.Warnings, "Preview does not reserve capacity or budget; current credentials, quota and DNS are checked at dispatch")
	return result, nil
}
