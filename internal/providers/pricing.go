package providers

import "exta/internal/domain"

// ReservationAmount is a conservative per-call bound for the supported wire payload.
// Pricing snapshot: 2026-10-22, https://exa.ai/docs/admin/pricing and the approved
// Tavily credits source. Account configuration may raise this floor. This local
// admission bound is not an upstream hard spending cap or an invoice prediction.
func ReservationAmount(provider domain.Provider, input Input) int64 {
	if provider == "exa" {
		if input.Operation == "fetch" {
			return int64(len(input.Urls)) * 1000
		}
		base := int64(7000)
		if input.Profile == "exa_deep" {
			base = 12000
		}
		return base + max(int64(0), int64(input.MaxResults)-10)*1000
	}
	switch input.Operation {
	case "search", "advanced_search":
		if input.Profile == "tavily_advanced" {
			return 2
		}
		return 1
	case "fetch":
		return (int64(len(input.Urls)) + 4) / 5
	case "map", "crawl":
		mapping := (int64(input.Limit) + 9) / 10
		if input.Instructions != "" {
			mapping *= 2
		}
		if input.Operation == "map" {
			return mapping
		}
		extraction := (int64(input.Limit) + 4) / 5
		if input.ExtractionProfile == "advanced" {
			extraction *= 2
		}
		return mapping + extraction
	}
	return 0
}
