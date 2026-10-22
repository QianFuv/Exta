package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"exta/internal/domain"
	"exta/internal/providers"
	"exta/internal/store"
)

// ExaUsageConfig holds only local references and upstream identifiers, never secrets.
// The service key is a disabled retrieval-pool entry belonging to the same account.
type ExaUsageConfig struct {
	AccountId      string            `json:"account_id"`
	ServiceKeyId   string            `json:"service_key_id"`
	UpstreamKeyIds map[string]string `json:"upstream_key_ids"`
}

type usageFlight struct {
	done   chan struct{}
	result domain.UsageRefreshResult
	err    error
}

// UsageService provides bounded, account-coalesced read-only usage refreshes.
type UsageService struct {
	Store     *store.Store
	Clients   map[domain.Provider]*providers.Client
	ExaClient *providers.Client
	ExaConfig []ExaUsageConfig
	mutex     sync.Mutex
	flights   map[string]*usageFlight
}

// Refresh joins an identical active query or persists the account cooldown before dispatch.
func (service *UsageService) Refresh(ctx context.Context, input domain.UsageRefresh) (domain.UsageRefreshResult, error) {
	var result domain.UsageRefreshResult
	if err := domain.ValidateJSON("UsageRefresh", []byte(store.JSON(input))); err != nil {
		return result, store.Reject("invalid_request", 400, "Invalid usage refresh selection")
	}
	if service.Store.Vault == nil {
		return result, store.Reject("secret_store_locked", 503, "Secret store is locked")
	}
	accounts, err := service.Store.Accounts(ctx)
	if err != nil {
		return result, err
	}
	keys, err := service.Store.Keys(ctx)
	if err != nil {
		return result, err
	}
	selected := []domain.Account{}
	selectedKeys := map[string][]domain.ProviderKey{}
	for _, account := range accounts {
		if input.AccountId != nil && *input.AccountId != account.Id {
			continue
		}
		for _, key := range keys {
			if key.AccountId == account.Id && (input.KeyId == nil || *input.KeyId == key.Id) {
				selectedKeys[account.Id] = append(selectedKeys[account.Id], key)
			}
		}
		if input.KeyId == nil || len(selectedKeys[account.Id]) > 0 {
			selected = append(selected, account)
		}
	}
	if len(selected) == 0 && (input.AccountId != nil || input.KeyId != nil) {
		return result, store.Reject("not_found", 404, "Usage selection was not found")
	}
	count := 0
	ids := []string{}
	for _, account := range selected {
		count += len(selectedKeys[account.Id])
		ids = append(ids, account.Id)
	}
	if count > 100 || len(ids) > 100 {
		return result, store.Reject("invalid_request", 400, "Refresh at most 100 keys/accounts; select a narrower scope")
	}
	sort.Strings(ids)
	flightID := strings.Join(ids, ",") + "/"
	if input.KeyId != nil {
		flightID += *input.KeyId
	}
	service.mutex.Lock()
	if flight := service.flights[flightID]; flight != nil {
		service.mutex.Unlock()
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-flight.done:
			result = flight.result
			result.Status = "coalesced"
			return result, flight.err
		}
	}
	if service.flights == nil {
		service.flights = map[string]*usageFlight{}
	}
	flight := &usageFlight{done: make(chan struct{})}
	service.flights[flightID] = flight
	service.mutex.Unlock()
	defer func() {
		service.mutex.Lock()
		delete(service.flights, flightID)
		close(flight.done)
		service.mutex.Unlock()
	}()
	next, err := service.Store.ClaimUsageRefresh(ctx, ids)
	if err != nil {
		flight.err = err
		return result, err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, account := range selected {
		for _, key := range selectedKeys[account.Id] {
			if bounded.Err() != nil {
				flight.err = bounded.Err()
				return result, flight.err
			}
			deadline, limited, refreshErr := service.refreshKey(bounded, account, key)
			if refreshErr != nil {
				err = refreshErr
				flight.err = err
				return result, err
			}
			if deadline.After(next) {
				next = deadline
			}
			if limited {
				break
			}
		}
	}
	result = domain.UsageRefreshResult{Status: "refreshed", ObservedAt: store.Stamp(service.Store.Now()), NextRefreshAt: store.Stamp(next)}
	flight.result = result
	return result, nil
}

func (service *UsageService) refreshKey(ctx context.Context, account domain.Account, key domain.ProviderKey) (time.Time, bool, error) {
	sharedUntil, err := service.Store.ProviderUsageDeadline(ctx, string(account.Provider))
	if err != nil || sharedUntil.After(service.Store.Now()) {
		return sharedUntil, true, err
	}
	client := service.Clients[account.Provider]
	credentialKey, upstreamID, isDedicated := key.Id, "", false
	reason := ""
	if account.Provider == "exa" {
		client = service.ExaClient
		reason = "service_credential_not_configured"
		for _, config := range service.ExaConfig {
			if config.AccountId == account.Id && config.UpstreamKeyIds[key.Id] != "" && config.ServiceKeyId != key.Id {
				credentialKey, upstreamID, isDedicated = config.ServiceKeyId, config.UpstreamKeyIds[key.Id], true
				reason = ""
			}
		}
	}
	var credential string
	if reason == "" {
		var err error
		credential, err = service.Store.UsageCredential(ctx, account.Id, credentialKey, isDedicated)
		if err != nil {
			reason = "usage_credential_unavailable"
		}
	}
	if client == nil {
		reason = "usage_client_unavailable"
	}
	observed := store.Stamp(service.Store.Now())
	if reason != "" {
		return time.Time{}, false, service.Store.SaveUsage(ctx, []domain.AccountUsage{{AccountId: account.Id, KeyId: &key.Id, Provider: account.Provider, Unit: account.QuotaPolicy.Unit, Source: "upstream", ObservedAt: &observed, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason}})
	}
	next, err := service.Store.ExtendUsageCooldown(ctx, account.Id, 2*time.Minute)
	if err != nil {
		return next, false, err
	}
	call, cancel := context.WithTimeout(ctx, 15*time.Second)
	items, outcome := client.Usage(call, credential, account.Id, key.Id, upstreamID)
	cancel()
	persist, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistCancel()
	limited := outcome.GateKind == "rate" || outcome.Status == 429
	if limited {
		next, err = service.Store.ExtendUsageCooldown(persist, account.Id, outcome.Cooldown)
		if err != nil {
			return next, true, err
		}
		if outcome.SharedCooldown {
			sharedUntil, err = service.Store.ExtendProviderUsageCooldown(persist, string(account.Provider), outcome.Cooldown)
			if err != nil {
				return next, true, err
			}
			if sharedUntil.After(next) {
				next = sharedUntil
			}
		}
	}
	if len(items) == 0 {
		reason = "usage_configuration_invalid"
		items = []domain.AccountUsage{{AccountId: account.Id, KeyId: &key.Id, Provider: account.Provider, Unit: account.QuotaPolicy.Unit, Source: "upstream", ObservedAt: &observed, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason}}
	}
	return next, limited, service.Store.SaveUsage(persist, items)
}

// ValidateExaUsage rejects duplicate mappings and malformed IDs before runtime startup.
func ValidateExaUsage(configs []ExaUsageConfig) error {
	seen := map[string]bool{}
	identifier := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	for _, config := range configs {
		if !identifier.MatchString(config.AccountId) || !identifier.MatchString(config.ServiceKeyId) || seen[config.AccountId] || len(config.UpstreamKeyIds) > 100 {
			return store.Reject("invalid_request", 400, "Invalid Exa usage references")
		}
		seen[config.AccountId] = true
		for localID, upstreamID := range config.UpstreamKeyIds {
			for _, value := range []string{localID, upstreamID} {
				if !identifier.MatchString(value) || localID == config.ServiceKeyId {
					return store.Reject("invalid_request", 400, "Invalid Exa usage identifier")
				}
			}
		}
	}
	return nil
}
