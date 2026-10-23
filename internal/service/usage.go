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
// The service key is a disabled Exa retrieval-pool entry used only for management.
type ExaUsageConfig struct {
	ServiceKeyId   string            `json:"service_key_id"`
	UpstreamKeyIds map[string]string `json:"upstream_key_ids"`
}

type usageFlight struct {
	done   chan struct{}
	result domain.UsageRefreshResult
	err    error
}

// UsageService provides bounded, key-coalesced read-only usage refreshes.
type UsageService struct {
	Store     *store.Store
	Clients   map[domain.Provider]*providers.Client
	ExaClient *providers.Client
	ExaConfig []ExaUsageConfig
	mutex     sync.Mutex
	flights   map[string]*usageFlight
}

// Refresh joins an identical active query or persists the key refresh cooldown before dispatch.
func (service *UsageService) Refresh(ctx context.Context, input domain.KeyUsageRefresh) (domain.UsageRefreshResult, error) {
	var result domain.UsageRefreshResult
	if err := domain.ValidateJSON("KeyUsageRefresh", []byte(store.JSON(input))); err != nil {
		return result, store.Reject("invalid_request", 400, "Invalid usage refresh selection")
	}
	if service.Store.Vault == nil {
		return result, store.Reject("secret_store_locked", 503, "Secret store is locked")
	}
	keys, err := service.Store.Keys(ctx)
	if err != nil {
		return result, err
	}
	selected := []domain.UpstreamKey{}
	ids := []string{}
	for _, key := range keys {
		if input.KeyId != nil && *input.KeyId != key.Id {
			continue
		}
		selected = append(selected, key)
		ids = append(ids, key.Id)
	}
	if len(selected) == 0 && input.KeyId != nil {
		return result, store.Reject("not_found", 404, "Usage key was not found")
	}
	if len(selected) > 100 {
		return result, store.Reject("invalid_request", 400, "Refresh at most 100 keys")
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
	limitedScopes := map[string]bool{}
	for _, key := range selected {
		if limitedScopes[key.AssociationRef] {
			continue
		}
		if bounded.Err() != nil {
			flight.err = bounded.Err()
			return result, flight.err
		}
		deadline, limited, refreshErr := service.refreshKey(bounded, key)
		if refreshErr != nil {
			flight.err = refreshErr
			return result, refreshErr
		}
		if deadline.After(next) {
			next = deadline
		}
		if limited {
			limitedScopes[key.AssociationRef] = true
		}
	}
	result = domain.UsageRefreshResult{Status: "refreshed", ObservedAt: store.Stamp(service.Store.Now()), NextRefreshAt: store.Stamp(next)}
	flight.result = result
	return result, nil
}

func (service *UsageService) refreshKey(ctx context.Context, key domain.UpstreamKey) (time.Time, bool, error) {
	sharedUntil, err := service.Store.ProviderUsageDeadline(ctx, string(key.Provider))
	if err != nil || sharedUntil.After(service.Store.Now()) {
		return sharedUntil, true, err
	}
	client := service.Clients[key.Provider]
	credentialKey, upstreamID, isDedicated := key.Id, "", false
	reason := ""
	if key.Provider == "exa" {
		client = service.ExaClient
		reason = "service_credential_not_configured"
		for _, config := range service.ExaConfig {
			if config.UpstreamKeyIds[key.Id] != "" && config.ServiceKeyId != key.Id {
				credentialKey, upstreamID, isDedicated = config.ServiceKeyId, config.UpstreamKeyIds[key.Id], true
				reason = ""
			}
		}
	}
	var credential string
	if reason == "" {
		var err error
		credential, err = service.Store.UsageCredential(ctx, credentialKey, isDedicated)
		if err != nil {
			reason = "usage_credential_unavailable"
		}
	}
	if client == nil {
		reason = "usage_client_unavailable"
	}
	observed := store.Stamp(service.Store.Now())
	if reason != "" {
		return time.Time{}, false, service.Store.SaveUsage(ctx, []domain.KeyUsage{{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: "key", KeyId: &key.Id, Provider: key.Provider, Unit: key.QuotaPolicy.Unit, Source: "upstream", ObservedAt: &observed, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason}})
	}
	next, err := service.Store.ExtendUsageCooldown(ctx, key.AssociationRef, 2*time.Minute)
	if err != nil {
		return next, false, err
	}
	call, cancel := context.WithTimeout(ctx, 15*time.Second)
	items, outcome := client.Usage(call, credential, key.AssociationRef, key.Id, upstreamID)
	cancel()
	persist, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistCancel()
	limited := outcome.GateKind == "rate" || outcome.Status == 429
	if limited {
		next, err = service.Store.ExtendUsageCooldown(persist, key.AssociationRef, outcome.Cooldown)
		if err != nil {
			return next, true, err
		}
		if outcome.SharedCooldown {
			sharedUntil, err = service.Store.ExtendProviderUsageCooldown(persist, string(key.Provider), outcome.Cooldown)
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
		items = []domain.KeyUsage{{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: "key", KeyId: &key.Id, Provider: key.Provider, Unit: key.QuotaPolicy.Unit, Source: "upstream", ObservedAt: &observed, Freshness: "unknown", LimitKind: "unknown", UnavailableReason: &reason}}
	}
	observations := make([]domain.KeyUsage, 0, len(items))
	for _, item := range items {
		scope := "key"
		if item.KeyId == nil {
			scope = "shared"
		}
		observations = append(observations, domain.KeyUsage{AssociationRef: key.AssociationRef, AssociationEvidence: key.AssociationEvidence, Scope: scope, KeyId: item.KeyId, Provider: item.Provider, Source: item.Source, ObservedAt: item.ObservedAt, Freshness: item.Freshness, Unit: item.Unit, PeriodStart: item.PeriodStart, PeriodEnd: item.PeriodEnd, Used: item.Used, Reserved: item.Reserved, LimitKind: item.LimitKind, Limit: item.Limit, Remaining: item.Remaining, UnavailableReason: item.UnavailableReason})
	}
	return next, limited, service.Store.SaveUsage(persist, observations)
}

// ValidateExaUsage rejects duplicate mappings and malformed IDs before runtime startup.
func ValidateExaUsage(configs []ExaUsageConfig) error {
	seen := map[string]bool{}
	mappedKeys := map[string]bool{}
	identifier := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	for _, config := range configs {
		if !identifier.MatchString(config.ServiceKeyId) || seen[config.ServiceKeyId] || len(config.UpstreamKeyIds) > 100 {
			return store.Reject("invalid_request", 400, "Invalid Exa usage references")
		}
		seen[config.ServiceKeyId] = true
		for localID, upstreamID := range config.UpstreamKeyIds {
			if mappedKeys[localID] {
				return store.Reject("invalid_request", 400, "Exa usage key has multiple mappings")
			}
			mappedKeys[localID] = true
			for _, value := range []string{localID, upstreamID} {
				if !identifier.MatchString(value) || localID == config.ServiceKeyId {
					return store.Reject("invalid_request", 400, "Invalid Exa usage identifier")
				}
			}
		}
	}
	return nil
}
