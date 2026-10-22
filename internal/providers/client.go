// Package providers translates audited provider protocols without hidden retries.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/store"
)

// Input contains schema-validated, semantically normalized retrieval intent.
type Input struct {
	Operation         domain.Operation `json:"-"`
	Profile           string           `json:"profile"`
	Query             string           `json:"query"`
	MaxResults        domain.Integer   `json:"max_results"`
	ContentMode       string           `json:"content_mode"`
	IncludeDomains    []string         `json:"include_domains"`
	ExcludeDomains    []string         `json:"exclude_domains"`
	ProviderOptions   json.RawMessage  `json:"provider_options"`
	Urls              []string         `json:"urls"`
	Url               string           `json:"url"`
	Format            string           `json:"format"`
	MaxChars          domain.Integer   `json:"max_chars"`
	MaxDepth          domain.Integer   `json:"max_depth"`
	MaxBreadth        domain.Integer   `json:"max_breadth"`
	Limit             domain.Integer   `json:"limit"`
	SelectPaths       []string         `json:"select_paths"`
	ExcludePaths      []string         `json:"exclude_paths"`
	SelectDomains     []string         `json:"select_domains"`
	Instructions      string           `json:"instructions"`
	ExtractionProfile string           `json:"extraction_profile"`
}

// Outcome separates response data, transmission evidence, retry policy and accounting.
type Outcome struct {
	Results                         []domain.RetrievalResult
	Failures                        []domain.ItemFailure
	Output                          *json.RawMessage
	Warnings                        []string
	Cost                            *int64
	Certainty                       string
	Transmission                    string
	Status                          int
	RequestId                       *string
	Duration                        time.Duration
	Failure                         *store.Fault
	CanRetry, IsUnspent             bool
	RetryProhibited, SharedCooldown bool
	GateScope, GateKind             string
	Cooldown                        time.Duration
}

// Client always addresses a fixed provider origin; transport injection is for local fixtures.
type Client struct {
	Provider  domain.Provider
	HTTP      *http.Client
	traversal *http.Client
	origin    string
	Now       func() time.Time
}

// New creates a provider client with audited egress or an explicitly injected test transport.
func New(provider domain.Provider, transport http.RoundTripper) (*Client, error) {
	origin := "https://api.exa.ai"
	if provider == "tavily" {
		origin = "https://api.tavily.com"
	} else if provider != "exa" {
		return nil, store.Reject("unsupported_capability", 422, "Unknown provider")
	}
	client, err := security.ProviderClient(origin)
	if err != nil {
		return nil, err
	}
	configured := client.Transport.(*http.Transport)
	dial := configured.DialContext
	configured.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return dial(bounded, network, address)
	}
	configured.TLSHandshakeTimeout = 5 * time.Second
	configured.ResponseHeaderTimeout = 30 * time.Second
	traversal := *client
	traversalTransport := configured.Clone()
	traversalTransport.ResponseHeaderTimeout = 155 * time.Second
	traversal.Transport = traversalTransport
	if transport != nil {
		client.Transport = transport
		traversal.Transport = transport
	}
	return &Client{Provider: provider, HTTP: client, traversal: &traversal, origin: origin, Now: time.Now}, nil
}

var safeIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var decimalNumber = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)

// Amount parses decimal accounting without float64. Dollar fractions round upward
// to a micro-dollar; fractional credit counts remain unknown instead of fabricated.
func Amount(raw json.RawMessage, scale int64) *int64 {
	if len(raw) == 0 || len(raw) > 64 || !decimalNumber.Match(raw) {
		return nil
	}
	amount, ok := new(big.Rat).SetString(string(raw))
	if !ok || amount.Sign() < 0 {
		return nil
	}
	amount.Mul(amount, new(big.Rat).SetInt64(scale))
	integer, remainder := new(big.Int), new(big.Int)
	integer.QuoRem(amount.Num(), amount.Denom(), remainder)
	if remainder.Sign() != 0 {
		if scale == 1 {
			return nil
		}
		integer.Add(integer, big.NewInt(1))
	}
	if !integer.IsInt64() || integer.Int64() > 9007199254740991 {
		return nil
	}
	value := integer.Int64()
	return &value
}

// RetryAfter accepts delta seconds or an HTTP date, bounded to one day of cooldown.
func RetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, 86400)) * time.Second
	}
	if until, err := http.ParseTime(value); err == nil {
		return max(time.Duration(0), min(until.Sub(now), 24*time.Hour))
	}
	return 0
}

// Exchange performs exactly one HTTP exchange and never returns an upstream body in an error.
func (client *Client) Exchange(ctx context.Context, method, path, credential string, payload any, limit int64) ([]byte, Outcome) {
	outcome := Outcome{Results: []domain.RetrievalResult{}, Failures: []domain.ItemFailure{}, Warnings: []string{}, Certainty: "unknown", Transmission: "not_sent", IsUnspent: true}
	encoded, err := json.Marshal(payload)
	if err != nil {
		outcome.Failure = store.Reject("invalid_request", 400, "Provider request cannot be encoded")
		return nil, outcome
	}
	var body io.Reader
	if method != http.MethodGet && payload != nil {
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.origin+path, body)
	if err != nil {
		outcome.Failure = store.Reject("invalid_request", 400, "Provider request cannot be constructed")
		return nil, outcome
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if client.Provider == "exa" {
		request.Header.Set("x-api-key", credential)
	} else {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	var connected atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}))
	started := time.Now()
	selected := client.HTTP
	if path == "/map" || path == "/crawl" {
		selected = client.traversal
	}
	response, err := selected.Do(request)
	outcome.Duration = time.Since(started)
	if connected.Load() {
		outcome.Transmission = "possibly_sent"
		outcome.IsUnspent = false
	}
	if err != nil {
		outcome.CanRetry = outcome.IsUnspent
		outcome.GateScope = "account"
		outcome.GateKind = "transient"
		outcome.Failure = store.Reject("upstream_failure", 502, "Provider exchange failed")
		if ctx.Err() != nil {
			outcome.Failure = store.Reject("deadline_exceeded", 504, "Operation deadline or caller cancellation ended the exchange")
		}
		return nil, outcome
	}
	defer response.Body.Close()
	outcome.Transmission = "response_received"
	outcome.IsUnspent = false
	outcome.Status = response.StatusCode
	outcome.RetryProhibited = response.StatusCode == 400 || response.StatusCode == 403 || response.StatusCode == 422
	if response.StatusCode == 429 {
		outcome.GateScope, outcome.GateKind, outcome.SharedCooldown = "account", "rate", true
		outcome.Cooldown = RetryAfter(response.Header.Get("Retry-After"), client.Now())
		if outcome.Cooldown == 0 {
			outcome.Cooldown = time.Second
		}
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	outcome.Duration = time.Since(started)
	if readErr != nil {
		outcome.Failure = store.Reject("upstream_protocol_error", 502, "Provider response was incomplete")
		return nil, outcome
	}
	if int64(len(data)) > limit {
		outcome.Failure = store.Reject("response_too_large", 502, "Provider response exceeds the configured limit")
		return nil, outcome
	}
	var envelope struct {
		RequestId string `json:"requestId"`
		SnakeId   string `json:"request_id"`
		Tag       string `json:"tag"`
		Error     struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	if strings.HasPrefix(path, "/agent/") && envelope.Tag == "" {
		envelope.Tag = envelope.Error.Code
	}
	id := envelope.RequestId
	if id == "" {
		id = envelope.SnakeId
	}
	if safeIdentifier.MatchString(id) {
		outcome.RequestId = &id
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		outcome.Failure = store.Reject("upstream_failure", 502, "Provider rejected the request")
		outcome.Failure.Details["upstream_http_status"] = response.StatusCode
		if safeIdentifier.MatchString(envelope.Tag) {
			outcome.Failure.Details["upstream_tag"] = envelope.Tag
		}
		switch response.StatusCode {
		case 400, 422:
			outcome.IsUnspent = true
		case 401:
			outcome.IsUnspent = true
			outcome.CanRetry = true
			outcome.GateScope = "key"
			outcome.GateKind = "invalid"
		case 403:
			outcome.IsUnspent = true
		case 402, 432, 433:
			outcome.IsUnspent = true
			outcome.GateScope = "account"
			outcome.GateKind = "quota"
			outcome.Failure = store.Reject("quota_unavailable", 503, "Provider allocation is unavailable")
			if client.Provider == "exa" && response.StatusCode == 402 {
				outcome.CanRetry = true
				if envelope.Tag == "API_KEY_BUDGET_EXCEEDED" {
					outcome.GateScope = "key"
				}
			}
			if client.Provider == "tavily" && (response.StatusCode == 432 || response.StatusCode == 433) {
				outcome.CanRetry = true
			}
		case 429:
			outcome.IsUnspent = true
			outcome.CanRetry = true
			outcome.GateScope = "account"
			outcome.GateKind = "rate"
			outcome.SharedCooldown = client.Provider != "exa" || envelope.Tag != "RATE_LIMIT_EXCEEDED"
			outcome.Cooldown = RetryAfter(response.Header.Get("Retry-After"), client.Now())
			if outcome.Cooldown == 0 {
				outcome.Cooldown = time.Second
			}
			outcome.Failure = store.Reject("upstream_rate_limited", 429, "Provider rate allowance is exhausted")
			outcome.Failure.RetryAfterMs = outcome.Cooldown.Milliseconds()
		default:
			if response.StatusCode >= 500 {
				outcome.GateScope = "account"
				outcome.GateKind = "transient"
			}
			if client.Provider == "exa" && response.StatusCode == 503 && envelope.Tag == "SERVICE_OVERLOADED" {
				outcome.IsUnspent = true
				outcome.CanRetry = true
			}
		}
		return nil, outcome
	}
	if !json.Valid(data) {
		outcome.Failure = store.Reject("upstream_protocol_error", 502, "Provider returned invalid JSON")
		return nil, outcome
	}
	return data, outcome
}
