package security

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"exta/internal/store"
	"golang.org/x/net/idna"
)

// Resolver permits deterministic DNS fixtures without making external test calls.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// UrlPolicy validates user targets without fetching their content locally.
type UrlPolicy struct{ Resolver Resolver }

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

// PublicAddress rejects local, reserved, mapped and translation addresses.
func PublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Is4In6() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range reservedNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// Hostname canonicalizes IDNs and rejects ambiguous numeric/internal names.
func Hostname(value string) (string, error) {
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(value), "."))
	if err != nil || len(host) > 253 || !strings.Contains(host, ".") {
		return "", store.Reject("target_blocked", 422, "Public DNS hostname is required")
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home", ".test", ".invalid", ".example"} {
		if strings.HasSuffix(host, suffix) {
			return "", store.Reject("target_blocked", 422, "Internal hostname is not allowed")
		}
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", store.Reject("target_blocked", 422, "Invalid hostname")
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return "", store.Reject("target_blocked", 422, "Invalid hostname")
			}
		}
	}
	last := host[strings.LastIndex(host, ".")+1:]
	if last[0] >= '0' && last[0] <= '9' || strings.HasPrefix(last, "0x") {
		return "", store.Reject("target_blocked", 422, "Ambiguous numeric hostname is not allowed")
	}
	return host, nil
}

// Validate normalizes a target, requires every DNS answer to be public and bounds DNS time.
func (policy UrlPolicy) Validate(ctx context.Context, value string) (string, error) {
	address, err := url.Parse(value)
	if err != nil || address.User != nil || address.Opaque != "" || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") || strings.Contains(address.Host, "%") || strings.Contains(value, "\\") {
		return "", store.Reject("target_blocked", 422, "Only public HTTP(S) URLs without user information are allowed")
	}
	port := address.Port()
	if port != "" && port != "80" && port != "443" {
		return "", store.Reject("target_blocked", 422, "Only ports 80 and 443 are allowed")
	}
	host := address.Hostname()
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !PublicAddress(ip) {
			return "", store.Reject("target_blocked", 422, "Target address is not public")
		}
		host = ip.String()
	} else {
		host, err = Hostname(host)
		if err != nil {
			return "", err
		}
		if _, err = policy.resolve(ctx, host); err != nil {
			return "", err
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" && !(address.Scheme == "http" && port == "80" || address.Scheme == "https" && port == "443") {
		host += ":" + port
	}
	address.Host = host
	address.Fragment = ""
	if address.Path == "" {
		address.Path = "/"
	}
	return address.String(), nil
}

func (policy UrlPolicy) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := resolver.LookupNetIP(bounded, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, store.Reject("target_blocked", 422, "Target DNS could not be validated")
	}
	for _, address := range addresses {
		if !PublicAddress(address) {
			return nil, store.Reject("target_blocked", 422, "Target DNS includes a non-public address")
		}
	}
	return addresses, nil
}

// ProviderClient fixes local egress to one audited HTTPS origin. It ignores proxy
// environment variables, checks every fresh DNS answer and dials only a checked IP.
// Test providers are injected at the adapter boundary, never through runtime config.
func ProviderClient(origin string) (*http.Client, error) {
	address, err := url.Parse(origin)
	if err != nil || address.Scheme != "https" || address.User != nil || address.Path != "" || address.RawQuery != "" || address.Fragment != "" || address.Port() != "" {
		return nil, errors.New("provider origin must be fixed HTTPS")
	}
	if address.Host != "api.exa.ai" && address.Host != "api.tavily.com" {
		return nil, errors.New("provider origin is not approved")
	}
	policy := UrlPolicy{}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 32, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 150 * time.Second}
	transport.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(target)
		if err != nil || host != address.Host || port != "443" {
			return nil, errors.New("provider destination rejected")
		}
		addresses, err := policy.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var last error
		for _, ip := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
