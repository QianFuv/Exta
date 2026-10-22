package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

// NewExaUsage creates a dedicated read-only team-management client, never a pool fallback.
func NewExaUsage(transport http.RoundTripper) (*Client, error) {
	client, err := security.ProviderClient("https://admin-api.exa.ai")
	if err != nil {
		return nil, err
	}
	if transport != nil {
		client.Transport = transport
	}
	client.Timeout = 15 * time.Second
	return &Client{Provider: "exa", HTTP: client, origin: "https://admin-api.exa.ai", Now: time.Now}, nil
}

func usageValue(raw json.RawMessage) *domain.Integer {
	value := Amount(raw, 1)
	if value == nil {
		return nil
	}
	converted := domain.Integer(*value)
	return &converted
}

func usageTotal(first, second json.RawMessage) *domain.Integer {
	left, right := usageValue(first), usageValue(second)
	if left == nil || right == nil || int64(*left)+int64(*right) > 9007199254740991 {
		return nil
	}
	total := *left + *right
	return &total
}

func usageLimit(item *domain.AccountUsage, limit *domain.Integer, unlimited bool) {
	item.LimitKind = "unknown"
	if unlimited {
		item.LimitKind = "unlimited"
	} else if limit != nil {
		item.LimitKind, item.Limit = "finite", limit
		if item.Used != nil {
			remaining := max(domain.Integer(0), *limit-*item.Used)
			item.Remaining = &remaining
		}
	}
}

// Usage returns separate key/account observations. Unknown values never become zero.
// The Exa path requires a dedicated client and an explicit upstream key identifier.
func (client *Client) Usage(ctx context.Context, credential, accountID, keyID, upstreamKeyID string) ([]domain.AccountUsage, Outcome) {
	path := "/usage"
	if client.Provider == "exa" {
		if client.origin != "https://admin-api.exa.ai" || !safeIdentifier.MatchString(upstreamKeyID) {
			return nil, Outcome{Failure: store.Reject("invalid_request", 400, "Exa usage requires an explicit service credential and upstream key identifier")}
		}
		path = "/team-management/api-keys/" + url.PathEscape(upstreamKeyID) + "/usage"
	}
	body, outcome := client.Exchange(ctx, http.MethodGet, path, credential, nil, 1<<20)
	observed := store.Stamp(client.Now())
	unit := domain.UsageUnit("micro_usd")
	if client.Provider == "tavily" {
		unit = "tavily_credit"
	}
	key := domain.AccountUsage{AccountId: accountID, KeyId: &keyID, Provider: client.Provider, Unit: unit, Source: "upstream", Freshness: "unknown", ObservedAt: &observed, LimitKind: "unknown"}
	account := key
	account.KeyId = nil
	items := []domain.AccountUsage{key}
	if client.Provider == "tavily" {
		items = append(items, account)
	}
	if outcome.Failure != nil || outcome.Status != 200 {
		reason := "usage_unavailable"
		if outcome.Failure != nil {
			reason = outcome.Failure.Code
		}
		if client.Provider == "exa" && (outcome.Status == 401 || outcome.Status == 403) {
			reason = "service_permission_missing"
		}
		for index := range items {
			items[index].UnavailableReason = &reason
		}
		return items, outcome
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return items, outcome
	}
	if client.Provider == "exa" {
		var returnedID string
		_ = json.Unmarshal(payload["id"], &returnedID)
		if returnedID != upstreamKeyID {
			reason := "upstream_identity_mismatch"
			items[0].UnavailableReason = &reason
			return items, outcome
		}
		if value := Amount(payload["total_cost_usd"], 1000000); value != nil {
			converted := domain.Integer(*value)
			items[0].Used = &converted
			items[0].Freshness = "fresh"
		}
		var period struct{ Start, End string }
		if json.Unmarshal(payload["period"], &period) == nil {
			start, firstErr := time.Parse(time.RFC3339Nano, period.Start)
			end, lastErr := time.Parse(time.RFC3339Nano, period.End)
			if firstErr == nil && lastErr == nil && !start.After(end) {
				items[0].PeriodStart, items[0].PeriodEnd = &period.Start, &period.End
			}
		}
		return items, outcome
	}
	var keyData, accountData map[string]json.RawMessage
	_ = json.Unmarshal(payload["key"], &keyData)
	_ = json.Unmarshal(payload["account"], &accountData)
	items[0].Used = usageValue(keyData["usage"])
	usageLimit(&items[0], usageValue(keyData["limit"]), string(keyData["limit"]) == "null")
	items[1].Used = usageTotal(accountData["plan_usage"], accountData["paygo_usage"])
	usageLimit(&items[1], usageTotal(accountData["plan_limit"], accountData["paygo_limit"]), false)
	for index := range items {
		if items[index].Used != nil {
			items[index].Freshness = "fresh"
		} else {
			reason := "usage_fields_unknown"
			items[index].UnavailableReason = &reason
		}
	}
	return items, outcome
}
