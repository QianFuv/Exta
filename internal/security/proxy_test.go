package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyPeersMustBeExplicitAndNarrow(t *testing.T) {
	for _, ips := range [][]string{nil, {}, {"0.0.0.0"}, {"::"}, {"172.30.0.0/24"}, {"npm"}, {"8.8.8.8"}, {"fe80::1%eth0"}, {"172.30.0.2", "::ffff:172.30.0.2"}} {
		if _, err := NewProxyPolicy(&ProxyConfig{IPs: ips}); err == nil {
			t.Errorf("accepted ambiguous or broad proxy configuration: %v", ips)
		}
	}
	for _, ips := range [][]string{{"172.30.0.2"}, {"fd00::2"}, {"127.0.0.1", "::1"}} {
		if _, err := NewProxyPolicy(&ProxyConfig{IPs: ips}); err != nil {
			t.Errorf("rejected exact private proxy: %v", err)
		}
	}
}

func TestProxyUsesSocketPeerAndRejectsAmbiguousHeaders(t *testing.T) {
	policy, err := NewProxyPolicy(&ProxyConfig{IPs: []string{"172.30.0.2", "fd00::2"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, peer       string
		protocol, client []string
		want             int
	}{
		{"valid", "172.30.0.2:4123", []string{"https"}, []string{"198.51.100.7"}, 204},
		{"mapped peer and client", "[::ffff:172.30.0.2]:4123", []string{"https"}, []string{"::ffff:198.51.100.7"}, 204},
		{"ipv6 peer", "[fd00::2]:4123", []string{"https"}, []string{"198.51.100.7"}, 204},
		{"untrusted spoof", "172.30.0.3:4123", []string{"https"}, []string{"198.51.100.7"}, 403},
		{"malformed peer", "172.30.0.2", []string{"https"}, []string{"198.51.100.7"}, 403},
		{"missing scheme", "172.30.0.2:4123", nil, []string{"198.51.100.7"}, 403},
		{"http scheme", "172.30.0.2:4123", []string{"http"}, []string{"198.51.100.7"}, 403},
		{"scheme chain", "172.30.0.2:4123", []string{"https,http"}, []string{"198.51.100.7"}, 403},
		{"duplicate scheme", "172.30.0.2:4123", []string{"https", "https"}, []string{"198.51.100.7"}, 403},
		{"missing client", "172.30.0.2:4123", []string{"https"}, nil, 403},
		{"client chain", "172.30.0.2:4123", []string{"https"}, []string{"198.51.100.7,198.51.100.8"}, 403},
		{"duplicate client", "172.30.0.2:4123", []string{"https"}, []string{"198.51.100.7", "198.51.100.7"}, 403},
		{"client zone", "172.30.0.2:4123", []string{"https"}, []string{"fe80::1%eth0"}, 403},
		{"client unspecified", "172.30.0.2:4123", []string{"https"}, []string{"0.0.0.0"}, 403},
		{"client multicast", "172.30.0.2:4123", []string{"https"}, []string{"ff02::1"}, 403},
	} {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://exta.example/healthz", nil)
			request.RemoteAddr = item.peer
			request.Header["X-Forwarded-Proto"] = item.protocol
			request.Header["X-Real-Ip"] = item.client
			request.Header.Set("Forwarded", "for=127.0.0.1;host=attacker.example;proto=http")
			request.Header.Set("X-Forwarded-Host", "attacker.example")
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			request.Header.Set("Origin", "https://exta.example")
			called := false
			handler := policy.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, current *http.Request) {
				called = true
				if ClientAddress(current) != "198.51.100.7" || current.RemoteAddr != item.peer || current.Host != "exta.example" || current.Header.Get("Origin") != "https://exta.example" {
					t.Error("trusted client or original security boundary changed")
				}
				for _, name := range []string{"Forwarded", "X-Forwarded-Host", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Proto"} {
					if current.Header.Get(name) != "" {
						t.Errorf("forwarding header escaped normalization: %s", name)
					}
				}
				writer.WriteHeader(204)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != item.want || called != (item.want == 204) {
				t.Fatalf("status=%d, called=%v", response.Code, called)
			}
			if request.Header.Get("X-Forwarded-Host") != "attacker.example" {
				t.Fatal("middleware mutated the original request headers")
			}
		})
	}
}

func TestDirectClientAddressIgnoresForwardingClaims(t *testing.T) {
	request := httptest.NewRequest("GET", "http://127.0.0.1/", nil)
	request.RemoteAddr = "[::ffff:192.0.2.9]:1234"
	request.Header.Set("X-Real-IP", "198.51.100.7")
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	if ClientAddress(request) != "192.0.2.9" {
		t.Fatal("direct caller changed its rate-limit address")
	}
}
