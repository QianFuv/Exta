package security

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"exta/internal/store"
)

// ProxyConfig explicitly enables HTTP behind the listed private reverse-proxy IPs.
type ProxyConfig struct {
	IPs []string `json:"ips"`
}

// ProxyPolicy trusts socket peers, never a claimed forwarding chain.
type ProxyPolicy struct {
	peers map[netip.Addr]bool
}

// NewProxyPolicy rejects broad networks, names and ambiguous proxy addresses.
func NewProxyPolicy(configuration *ProxyConfig) (*ProxyPolicy, error) {
	if configuration == nil {
		return nil, nil
	}
	if len(configuration.IPs) == 0 || len(configuration.IPs) > 16 {
		return nil, errors.New("trusted_proxy requires 1 through 16 exact private or loopback IPs")
	}
	policy := &ProxyPolicy{peers: make(map[netip.Addr]bool)}
	for _, value := range configuration.IPs {
		address, err := netip.ParseAddr(value)
		address = address.Unmap()
		if err != nil || address.Zone() != "" || (!address.IsPrivate() && !address.IsLoopback()) || policy.peers[address] {
			return nil, errors.New("trusted_proxy IPs must be unique exact private or loopback addresses")
		}
		policy.peers[address] = true
	}
	return policy, nil
}

type clientAddressKey struct{}

// ClientAddress returns the verified proxy client or the direct socket peer for rate limiting.
func ClientAddress(request *http.Request) string {
	if address, ok := request.Context().Value(clientAddressKey{}).(netip.Addr); ok {
		return address.String()
	}
	peer, err := netip.ParseAddrPort(request.RemoteAddr)
	if err != nil {
		return ""
	}
	return peer.Addr().Unmap().String()
}

// Wrap requires a trusted peer and unambiguous NPM-replaced headers before dispatch.
// Host, Origin and the actual peer remain untouched; external URL/cookie policy is configuration-owned.
func (policy *ProxyPolicy) Wrap(next http.Handler) http.Handler {
	if policy == nil {
		return next
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		peer, err := netip.ParseAddrPort(request.RemoteAddr)
		if err != nil || !policy.peers[peer.Addr().Unmap()] {
			WriteError(writer, request, store.Reject("origin_rejected", 403, "Request peer is not a trusted reverse proxy"))
			return
		}
		protocol := request.Header.Values("X-Forwarded-Proto")
		clients := request.Header.Values("X-Real-IP")
		if len(protocol) != 1 || protocol[0] != "https" || len(clients) != 1 {
			WriteError(writer, request, store.Reject("origin_rejected", 403, "Trusted proxy headers must identify HTTPS and one client IP"))
			return
		}
		client, err := netip.ParseAddr(clients[0])
		client = client.Unmap()
		if err != nil || client.Zone() != "" || client.IsUnspecified() || client.IsMulticast() {
			WriteError(writer, request, store.Reject("origin_rejected", 403, "Trusted proxy client IP is invalid"))
			return
		}
		forwarded := request.Clone(context.WithValue(request.Context(), clientAddressKey{}, client))
		for name := range forwarded.Header {
			lower := strings.ToLower(name)
			if lower == "forwarded" || lower == "x-real-ip" || lower == "cf-connecting-ip" || strings.HasPrefix(lower, "x-forwarded-") {
				delete(forwarded.Header, name)
			}
		}
		next.ServeHTTP(writer, forwarded)
	})
}
