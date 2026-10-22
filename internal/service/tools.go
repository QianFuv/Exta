package service

import (
	"context"
	"encoding/json"

	"exta/internal/domain"
	"exta/internal/store"
)

// CallTool dispatches canonical tools without transport-specific routing or admission.
func (service *Service) CallTool(ctx context.Context, identity store.Identity, name string, arguments json.RawMessage, id string) (any, error) {
	switch name {
	case "capabilities":
		return service.Capabilities(ctx)
	case "search", "advanced_search", "fetch", "map", "crawl":
		return service.Retrieve(ctx, identity.PrincipalId, id, name, arguments)
	case "research_create":
		var fields map[string]json.RawMessage
		if json.Unmarshal(arguments, &fields) != nil {
			return nil, store.Reject("invalid_request", 400, "Invalid research arguments")
		}
		var key string
		_ = json.Unmarshal(fields["idempotency_key"], &key)
		delete(fields, "idempotency_key")
		return service.Research.Accept(ctx, identity.PrincipalId, key, []byte(store.JSON(fields)))
	case "research_get", "research_list", "research_cancel", "research_resume_observation":
		var input struct {
			ID, Cursor, State  string
			EventsCursor       string         `json:"events_cursor"`
			EventsLimit        domain.Integer `json:"events_limit"`
			Limit              domain.Integer
			Mode               string
			ExpectedVersion    domain.Integer `json:"expected_version"`
			ObservationSeconds domain.Integer `json:"observation_seconds"`
		}
		if json.Unmarshal(arguments, &input) != nil {
			return nil, store.Reject("invalid_request", 400, "Invalid research arguments")
		}
		switch name {
		case "research_get":
			return service.Research.Get(ctx, identity.PrincipalId, input.ID, input.EventsCursor, int(input.EventsLimit))
		case "research_list":
			return service.Research.List(ctx, identity.PrincipalId, input.State, input.Cursor, int(input.Limit))
		case "research_cancel":
			return service.Research.Cancel(ctx, identity.PrincipalId, identity.PrincipalId, input.ID, domain.CancelRequest{Mode: input.Mode, ExpectedVersion: input.ExpectedVersion})
		default:
			return service.Research.Resume(ctx, identity.PrincipalId, identity.PrincipalId, input.ID, domain.ResumeRequest{ObservationSeconds: input.ObservationSeconds, ExpectedVersion: input.ExpectedVersion})
		}
	default:
		return nil, store.Reject("unsupported_capability", 422, "Unknown tool")
	}
}
