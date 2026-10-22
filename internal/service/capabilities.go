package service

import (
	"context"
	"encoding/json"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

// Capabilities describes implemented profiles and their current configured availability.
func (service *Service) Capabilities(ctx context.Context) (domain.Capabilities, error) {
	result := domain.Capabilities{ContractHash: domain.ContractSHA256, ContractVersion: "1.0.0", Capabilities: []domain.Capability{}}
	routes, err := service.Store.Routes(ctx)
	if err != nil {
		return result, err
	}
	settings, err := service.Store.Settings(ctx)
	if err != nil {
		return result, err
	}
	for _, route := range routes {
		item := domain.Capability{Operation: route.Capability, Profiles: []domain.CapabilityProfile{}, Constraints: json.RawMessage(`{}`), DefaultLimits: json.RawMessage(`{}`), HardLimits: json.RawMessage(`{}`)}
		operation := string(route.Capability)
		for _, provider := range []domain.Provider{"exa", "tavily"} {
			for _, profile := range []domain.Profile{"common", "exa_deep", "tavily_advanced", "exa_auto", "tavily_mini", "tavily_pro"} {
				target := domain.RouteTarget{Provider: provider, Profile: profile}
				if !SupportedTarget(operation, target) {
					continue
				}
				entry := domain.CapabilityProfile{Provider: provider, Profile: profile, BudgetModes: []string{}, Cancellation: domain.CancelCapability{}}
				minimum := providers.ReservationAmount(provider, providers.Input{Operation: route.Capability, Profile: string(profile), MaxResults: 10, Urls: []string{"https://example.com"}, Limit: 50, ExtractionProfile: "basic"})
				available, err := service.Store.Available(ctx, string(provider), minimum)
				if err != nil {
					return result, err
				}
				for _, configured := range route.Targets {
					if configured == target {
						entry.Enabled = route.Enabled && available && service.Clients[provider] != nil
					}
				}
				if operation == "research_create" {
					entry.Enabled = entry.Enabled && service.ResearchEnabled
					entry.Cancellation.Observation = true
					entry.Cancellation.Upstream = provider == "exa"
					if provider == "exa" {
						entry.BudgetModes = []string{"hard"}
					} else {
						entry.BudgetModes = []string{"estimated"}
						entry.Cancellation.UnavailableReason = pointer("Provider does not offer upstream cancellation")
					}
				}
				if !entry.Enabled {
					entry.UnavailableReason = pointer("Profile is not currently configured with an eligible account, credential and implementation")
				}
				item.Enabled = item.Enabled || entry.Enabled
				item.Profiles = append(item.Profiles, entry)
			}
		}
		defaults := map[string]any{"max_attempts": route.MaxAttempts, "deadline_seconds": route.DeadlineSeconds}
		var operational map[string]any
		if err := json.Unmarshal([]byte(store.JSON(settings.Limits)), &operational); err != nil {
			return result, err
		}
		for key, value := range operational {
			defaults[key] = value
		}
		hard := map[string]any{"max_attempts": 3}
		switch operation {
		case "search", "advanced_search":
			defaults["max_results"] = 10
			hard["max_results"] = 20
			defaults["content_mode"] = "snippet"
		case "fetch":
			hard["urls"] = 20
			defaults["max_chars"] = 20000
			hard["max_chars"] = 1000000
		case "map", "crawl":
			defaults["max_depth"] = 2
			defaults["max_breadth"] = 20
			defaults["allow_external"] = false
			defaults["limit"] = 50
			hard["limit"] = 100
			hard["max_depth"] = 3
			hard["max_breadth"] = 100
		case "research_create":
			defaults["profile"] = "exa_auto"
			defaults["micro_usd_cap"] = 1000000
			hard["observation_seconds"] = 3600
		}
		item.DefaultLimits = json.RawMessage(store.JSON(defaults))
		item.HardLimits = json.RawMessage(store.JSON(hard))
		item.Constraints = json.RawMessage(`{"explicit_profiles_preserved":true,"markdown_providers":["tavily"],"usage_units_not_additive":true}`)
		if !item.Enabled {
			item.UnavailableReason = pointer("No eligible configured route")
		}
		result.Capabilities = append(result.Capabilities, item)
	}
	return result, nil
}

// ErrorResponse gives every transport the same sanitized application error envelope.
func ErrorResponse(err error, id string) domain.ApiError {
	return domain.ApiError{Error: publicError(err, id)}
}
