package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"exta/internal/domain"
	"exta/internal/httpapi"
	"exta/internal/providers"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

type adminAction func(http.ResponseWriter, *http.Request, store.Identity, domain.Session) (any, error)

func managementHandler(state *store.Store, auth *security.Auth, configuration config) (http.Handler, error) {
	mux := http.NewServeMux()
	core, err := service.New(state, nil, security.UrlPolicy{})
	if err != nil {
		return nil, err
	}
	exaUsage, err := providers.NewExaUsage(nil)
	if err != nil {
		return nil, err
	}
	usage := &service.UsageService{Store: state, Clients: core.Clients, ExaClient: exaUsage, ExaConfig: configuration.ExaUsage}
	if err = httpapi.Register(mux, auth, core, usage, httpapi.Options{Origin: configuration.Origin, Version: version, Readiness: func() domain.Readiness { return readiness(state, configuration) }}); err != nil {
		return nil, err
	}
	register := func(pattern, permission string, status int, action adminAction) {
		mux.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
			identity, session, err := auth.Admin(request, permission)
			if err != nil {
				security.WriteError(writer, request, err)
				return
			}
			value, err := action(writer, request, identity, session)
			if err != nil {
				security.WriteError(writer, request, err)
				return
			}
			security.WriteJSON(writer, status, value)
		})
	}
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		security.WriteJSON(writer, 200, domain.Liveness{Status: "ok"})
	})
	mux.HandleFunc("GET /v1/ready", func(writer http.ResponseWriter, request *http.Request) {
		if _, err := auth.Client(request, "read"); err != nil {
			security.WriteError(writer, request, err)
			return
		}
		security.WriteJSON(writer, 200, readiness(state, configuration))
	})
	mux.HandleFunc("POST /v1/admin/session", func(writer http.ResponseWriter, request *http.Request) {
		if err := auth.CheckOrigin(request); err != nil {
			security.WriteError(writer, request, err)
			return
		}
		input, _, err := security.ReadJSON[domain.SessionCreate](writer, request, "SessionCreate", 4096)
		if err != nil {
			security.WriteError(writer, request, err)
			return
		}
		address, _, _ := net.SplitHostPort(request.RemoteAddr)
		session, token, err := state.Login(request.Context(), input.Credential, address)
		if err != nil {
			security.WriteError(writer, request, err)
			return
		}
		expires, _ := time.Parse(time.RFC3339Nano, session.ExpiresAt)
		auth.SessionCookie(writer, token, expires)
		security.WriteJSON(writer, 200, session)
	})
	register("GET /v1/admin/session", "authenticated_admin", 200, func(_ http.ResponseWriter, _ *http.Request, _ store.Identity, session domain.Session) (any, error) {
		return session, nil
	})
	register("DELETE /v1/admin/session", "authenticated_admin", 200, func(writer http.ResponseWriter, request *http.Request, _ store.Identity, _ domain.Session) (any, error) {
		cookie, _ := request.Cookie("exta_session")
		if err := state.Logout(request.Context(), cookie.Value); err != nil {
			return nil, err
		}
		auth.ClearSession(writer)
		return domain.Acknowledgement{Ok: true}, nil
	})
	register("GET /v1/admin/accounts", "admin:read", 200, func(_ http.ResponseWriter, request *http.Request, _ store.Identity, _ domain.Session) (any, error) {
		items, err := state.Accounts(request.Context())
		if err != nil {
			return nil, err
		}
		items, cursor, err := page(request, items, func(item domain.Account) string { return item.CreatedAt + "/" + item.Id })
		return domain.AccountList{Items: items, NextCursor: cursor}, err
	})
	register("POST /v1/admin/accounts", "admin:accounts:write", 201, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.AccountCreate](state, writer, request, "AccountCreate")
		if err != nil {
			return nil, err
		}
		return state.CreateAccount(request.Context(), identity.PrincipalId, input)
	})
	register("PATCH /v1/admin/accounts/{id}", "admin:accounts:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.AccountPatch](state, writer, request, "AccountPatch")
		if err != nil {
			return nil, err
		}
		return state.PatchAccount(request.Context(), identity.PrincipalId, request.PathValue("id"), input)
	})
	register("GET /v1/admin/keys", "admin:read", 200, func(_ http.ResponseWriter, request *http.Request, _ store.Identity, _ domain.Session) (any, error) {
		items, err := state.Keys(request.Context())
		if err != nil {
			return nil, err
		}
		items, cursor, err := page(request, items, func(item domain.ProviderKey) string { return item.CreatedAt + "/" + item.Id })
		return domain.ProviderKeyList{Items: items, NextCursor: cursor}, err
	})
	register("POST /v1/admin/keys", "admin:keys:write", 201, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.KeyCreate](state, writer, request, "KeyCreate")
		if err != nil {
			return nil, err
		}
		return state.AddKey(request.Context(), identity.PrincipalId, input)
	})
	register("PATCH /v1/admin/keys/{id}", "admin:keys:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.KeyPatch](state, writer, request, "KeyPatch")
		if err != nil {
			return nil, err
		}
		return state.ChangeKey(request.Context(), identity.PrincipalId, request.PathValue("id"), input.ExpectedVersion, input.Label, input.Enabled, nil, false)
	})
	register("POST /v1/admin/keys/{id}/rotate", "admin:keys:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.KeyRotate](state, writer, request, "KeyRotate")
		if err != nil {
			return nil, err
		}
		return state.ChangeKey(request.Context(), identity.PrincipalId, request.PathValue("id"), input.ExpectedVersion, nil, nil, &input.Secret, false)
	})
	register("POST /v1/admin/keys/{id}/retire", "admin:keys:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.VersionedAction](state, writer, request, "VersionedAction")
		if err != nil {
			return nil, err
		}
		return state.ChangeKey(request.Context(), identity.PrincipalId, request.PathValue("id"), input.ExpectedVersion, nil, nil, nil, true)
	})
	register("GET /v1/admin/routes", "admin:read", 200, func(_ http.ResponseWriter, request *http.Request, _ store.Identity, _ domain.Session) (any, error) {
		items, err := state.Routes(request.Context())
		return domain.RoutePolicyList{Items: items}, err
	})
	register("GET /v1/admin/tokens", "admin:read", 200, func(_ http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		items, err := state.Tokens(request.Context(), identity.PrincipalId)
		if err != nil {
			return nil, err
		}
		items, cursor, err := page(request, items, func(item domain.ClientToken) string { return item.CreatedAt + "/" + item.Id })
		return domain.ClientTokenList{Items: items, NextCursor: cursor}, err
	})
	register("POST /v1/admin/tokens", "admin:tokens:write", 201, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.TokenCreate](state, writer, request, "TokenCreate")
		if err != nil {
			return nil, err
		}
		return state.MintToken(request.Context(), identity.PrincipalId, input)
	})
	register("POST /v1/admin/tokens/{id}/revoke", "admin:tokens:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.VersionedAction](state, writer, request, "VersionedAction")
		if err != nil {
			return nil, err
		}
		return state.RevokeToken(request.Context(), identity.PrincipalId, request.PathValue("id"), input.ExpectedVersion)
	})
	settingsResult := func(value domain.Settings, err error) (any, error) {
		value.Build = buildInfo()
		value.Readiness = readiness(state, configuration)
		return value, err
	}
	register("GET /v1/admin/settings", "admin:read", 200, func(_ http.ResponseWriter, request *http.Request, _ store.Identity, _ domain.Session) (any, error) {
		value, err := state.Settings(request.Context())
		return settingsResult(value, err)
	})
	register("PATCH /v1/admin/settings", "admin:settings:write", 200, func(writer http.ResponseWriter, request *http.Request, identity store.Identity, _ domain.Session) (any, error) {
		input, err := body[domain.SettingsPatch](state, writer, request, "SettingsPatch")
		if err != nil {
			return nil, err
		}
		value, err := state.SaveSettings(request.Context(), identity.PrincipalId, input)
		return settingsResult(value, err)
	})
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		security.WriteError(writer, request, store.Reject("not_found", 404, "Resource not found"))
	})
	return auth.Guard(mux), nil
}

func body[T any](state *store.Store, writer http.ResponseWriter, request *http.Request, schema string) (T, error) {
	var empty T
	settings, err := state.Settings(request.Context())
	if err != nil {
		return empty, err
	}
	value, _, err := security.ReadJSON[T](writer, request, schema, int64(settings.Limits.RequestBytes))
	return value, err
}

func page[T any](request *http.Request, items []T, order func(T) string) ([]T, *string, error) {
	limit := 25
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			return nil, nil, store.Reject("invalid_request", 400, "Pagination limit must be 1 through 100")
		}
		limit = parsed
	}
	boundary := ""
	if cursor := request.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(decoded) > 256 || len(decoded) == 0 {
			return nil, nil, store.Reject("invalid_request", 400, "Invalid cursor")
		}
		boundary = string(decoded)
	}
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

func readiness(state *store.Store, configuration config) domain.Readiness {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := domain.Readiness{Database: "ready", SecretStore: "locked", Assets: "missing", BindScope: "loopback", Providers: []json.RawMessage{}}
	host, _, _ := net.SplitHostPort(configuration.Listen)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		result.BindScope = "nonloopback"
		result.NonloopbackSafetyConfigured = configuration.TlsCertFile != "" && configuration.TlsKeyFile != ""
	}
	if state.Vault != nil {
		result.SecretStore = "unlocked"
	}
	accounts, err := state.Accounts(ctx)
	if err != nil {
		result.Database = "unavailable"
		return result
	}
	keys, err := state.Keys(ctx)
	if err != nil {
		result.Database = "unavailable"
		return result
	}
	for _, provider := range []domain.Provider{"exa", "tavily"} {
		enabled := false
		for _, account := range accounts {
			if account.Provider != provider || !account.Enabled || account.QuotaPolicy.Allocation == 0 {
				continue
			}
			for _, key := range keys {
				if key.AccountId == account.Id && key.Enabled && key.Validity != "invalid" && key.RetirementState == "active" {
					enabled = true
				}
			}
		}
		var reason *string
		if !enabled {
			message := "No eligible configured account and key"
			reason = &message
		}
		result.Providers = append(result.Providers, json.RawMessage(store.JSON(map[string]any{"provider": provider, "enabled": enabled, "reason": reason})))
	}
	return result
}
