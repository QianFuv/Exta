// Package httpapi exposes shared use cases through authenticated HTTP routes.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"exta/api"
	"exta/internal/domain"
	mcpapi "exta/internal/mcp"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

// Options supplies local runtime metadata without exposing protected configuration.
type Options struct {
	Origin    string
	Version   string
	Readiness func() domain.Readiness
}

type action func(http.ResponseWriter, *http.Request, store.Identity) (any, error)

// Register adds retrieval, metadata and MCP routes to the existing management mux.
func Register(mux *http.ServeMux, auth *security.Auth, core *service.Service, usage *service.UsageService, options Options) error {
	registerResearch(mux, auth, core)
	register := func(pattern, permission string, admin bool, handler action) {
		mux.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
			var identity store.Identity
			var err error
			if admin {
				identity, _, err = auth.Admin(request, permission)
			} else {
				identity, err = auth.Client(request, permission)
			}
			if err != nil {
				security.WriteError(writer, request, err)
				return
			}
			result, err := handler(writer, request, identity)
			if err != nil {
				security.WriteError(writer, request, err)
				return
			}
			security.WriteJSON(writer, 200, result)
		})
	}
	for _, operation := range []string{"search", "advanced_search", "fetch", "map", "crawl"} {
		register("POST /v1/"+operation, "execute", false, func(writer http.ResponseWriter, request *http.Request, identity store.Identity) (any, error) {
			_, raw, err := Body[json.RawMessage](core.Store, writer, request, service.SchemaName(operation))
			if err != nil {
				return nil, err
			}
			return core.Retrieve(request.Context(), identity.PrincipalId, security.RequestID(request), operation, raw)
		})
	}
	register("GET /v1/capabilities", "read", false, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		return core.Capabilities(request.Context())
	})
	register("GET /v1/openapi.json", "read", false, func(_ http.ResponseWriter, _ *http.Request, _ store.Identity) (any, error) {
		return json.RawMessage(api.OpenAPI), nil
	})
	register("GET /v1/requests/{id}", "read", false, func(_ http.ResponseWriter, request *http.Request, identity store.Identity) (any, error) {
		return core.Store.Request(request.Context(), identity.PrincipalId, request.PathValue("id"))
	})
	register("PUT /v1/admin/routes/{capability}", "admin:routes:write", true, func(writer http.ResponseWriter, request *http.Request, identity store.Identity) (any, error) {
		input, _, err := Body[domain.RouteReplace](core.Store, writer, request, "RouteReplace")
		if err != nil {
			return nil, err
		}
		operation := request.PathValue("capability")
		if err = service.ValidateRoute(operation, input); err != nil {
			return nil, err
		}
		return core.Store.SaveRoute(request.Context(), identity.PrincipalId, operation, input)
	})
	register("POST /v1/admin/routes/preview", "admin:read", true, func(writer http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		_, raw, err := Body[json.RawMessage](core.Store, writer, request, "RoutePreviewRequest")
		if err != nil {
			return nil, err
		}
		return core.Preview(request.Context(), raw)
	})
	register("GET /v1/admin/requests", "admin:read", true, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		limit, err := Limit(request)
		if err != nil {
			return nil, err
		}
		query := request.URL.Query()
		filter := store.RequestFilter{Limit: limit, Cursor: query.Get("cursor"), Operation: query.Get("operation"), Provider: query.Get("provider"), Status: query.Get("status")}
		for _, pair := range [][2]string{{"Operation", filter.Operation}, {"Provider", filter.Provider}, {"RequestStatus", filter.Status}} {
			if pair[1] != "" {
				if err = domain.ValidateJSON(pair[0], []byte(store.JSON(pair[1]))); err != nil {
					return nil, store.Reject("invalid_request", 400, "Invalid request filter")
				}
			}
		}
		for _, pair := range []struct {
			name   string
			target *string
		}{{"created_after", &filter.From}, {"created_before", &filter.To}} {
			if value := query.Get(pair.name); value != "" {
				instant, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					return nil, store.Reject("invalid_request", 400, "Invalid request timestamp")
				}
				*pair.target = store.Stamp(instant)
			}
		}
		if filter.From != "" && filter.To != "" && filter.From > filter.To {
			return nil, store.Reject("invalid_request", 400, "Request time interval is reversed")
		}
		return core.Store.Requests(request.Context(), filter)
	})
	register("GET /v1/admin/requests/{id}", "admin:read", true, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		return core.Store.Request(request.Context(), "", request.PathValue("id"))
	})
	register("GET /v1/admin/usage", "admin:read", true, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		items, err := core.Store.Usage(request.Context())
		if err != nil {
			return nil, err
		}
		filtered := []domain.KeyUsage{}
		provider, selectedKey := request.URL.Query().Get("provider"), request.URL.Query().Get("key_id")
		if provider != "" {
			if err = domain.ValidateJSON("Provider", []byte(store.JSON(provider))); err != nil {
				return nil, store.Reject("invalid_request", 400, "Invalid provider filter")
			}
		}
		for _, item := range items {
			if (provider == "" || provider == string(item.Provider)) && (selectedKey == "" || item.KeyId != nil && selectedKey == *item.KeyId) {
				filtered = append(filtered, item)
			}
		}
		selected, cursor, err := Page(request, filtered, func(item domain.KeyUsage) string {
			key := ""
			if item.KeyId != nil {
				key = *item.KeyId
			}
			return item.AssociationRef + "/" + item.Source + "/" + key
		})
		if err != nil {
			return nil, err
		}
		_, waiting := core.Queue.Counts()
		overview, err := core.Store.Overview(request.Context(), waiting)
		return domain.KeyUsageView{Items: selected, NextCursor: cursor, Overview: overview}, err
	})
	register("POST /v1/admin/usage/refresh", "admin:usage:refresh", true, func(writer http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		input, _, err := Body[domain.KeyUsageRefresh](core.Store, writer, request, "KeyUsageRefresh")
		if err != nil {
			return nil, err
		}
		return usage.Refresh(request.Context(), input)
	})
	register("GET /v1/admin/connection-info", "admin:read", true, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		capabilities, err := core.Capabilities(request.Context())
		if err != nil {
			return nil, err
		}
		examples := []json.RawMessage{
			json.RawMessage(store.JSON(map[string]string{"title": "HTTP capabilities", "command": `curl -H "Authorization: Bearer <EXTA_TOKEN>" "` + options.Origin + `/v1/capabilities"`})),
			json.RawMessage(store.JSON(map[string]string{"title": "MCP discovery", "command": `curl -H "Authorization: Bearer <EXTA_TOKEN>" -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" -H "Mcp-Protocol-Version: 2026-07-28" -H "Mcp-Method: server/discover" --data '{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' "` + options.Origin + `/mcp"`})),
		}
		return domain.ConnectionInfo{Authentication: "preconfigured_bearer", Capabilities: capabilities, Examples: examples, McpUrl: options.Origin + "/mcp", RestBaseUrl: options.Origin + "/v1", OauthSupported: false, ProtocolVersions: []string{"2026-07-28", "2025-11-25"}, Readiness: options.Readiness()}, nil
	})
	register("GET /v1/admin/audit", "admin:audit:read", true, func(_ http.ResponseWriter, request *http.Request, _ store.Identity) (any, error) {
		limit, err := Limit(request)
		if err != nil {
			return nil, err
		}
		query := request.URL.Query()
		return core.Store.FilteredAudit(request.Context(), query.Get("cursor"), limit, query.Get("action"), query.Get("entity_id"))
	})
	mcpHandler, err := mcpapi.New(auth, core, options.Version)
	if err != nil {
		return err
	}
	mux.Handle("/mcp", mcpHandler)
	return nil
}

// Body applies the persisted byte limit and frozen validation without default expansion.
func Body[T any](state *store.Store, writer http.ResponseWriter, request *http.Request, schema string) (T, []byte, error) {
	var zero T
	settings, err := state.Settings(request.Context())
	if err != nil {
		return zero, nil, err
	}
	return security.ReadJSON[T](writer, request, schema, int64(settings.Limits.RequestBytes))
}

// Limit validates bounded metadata pagination.
func Limit(request *http.Request) (int, error) {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return 25, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 100 {
		return 0, store.Reject("invalid_request", 400, "Pagination limit must be 1 through 100")
	}
	return limit, nil
}

// Page returns a stable bounded page for small configuration-derived collections.
func Page[T any](request *http.Request, items []T, order func(T) string) ([]T, *string, error) {
	limit, err := Limit(request)
	if err != nil {
		return nil, nil, err
	}
	boundary, err := store.CursorBoundary(request.URL.Query().Get("cursor"))
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(items, func(first, second int) bool { return order(items[first]) > order(items[second]) })
	selected := []T{}
	for _, item := range items {
		if boundary == "" || order(item) < boundary {
			selected = append(selected, item)
			if len(selected) > limit {
				cursor := base64.RawURLEncoding.EncodeToString([]byte(order(selected[limit-1])))
				return selected[:limit], &cursor, nil
			}
		}
	}
	return selected, nil, nil
}
