package service

import (
	"context"
	"encoding/json"

	"exta/internal/store"
)

// CallTool dispatches canonical tools without transport-specific routing or admission.
func (service *Service) CallTool(ctx context.Context, identity store.Identity, name string, arguments json.RawMessage, id string) (any, error) {
	switch name {
	case "capabilities":
		return service.Capabilities(ctx)
	case "search", "advanced_search", "fetch", "map", "crawl":
		return service.Retrieve(ctx, identity.PrincipalId, id, name, arguments)
	default:
		return nil, store.Reject("provider_unavailable", 503, "Research service is not available")
	}
}
