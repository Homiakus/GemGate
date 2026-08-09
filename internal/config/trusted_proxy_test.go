package config

import "testing"

func TestTrustedProxyCIDRsAndSingleIPs(t *testing.T) {
	path := writeConfig(t, `
server:
  trusted_proxies:
    - 10.0.0.0/8
    - 192.0.2.10
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.TrustedProxies) != 2 {
		t.Fatalf("trusted proxy prefixes = %#v", rt.TrustedProxies)
	}
	if !rt.TrustedProxies[0].Contains(rt.TrustedProxies[0].Addr()) {
		t.Fatal("parsed CIDR does not contain its network address")
	}
	if got := rt.TrustedProxies[1].String(); got != "192.0.2.10/32" {
		t.Fatalf("single IP prefix = %q", got)
	}
}

func TestTrustedProxyRejectsInvalidCIDR(t *testing.T) {
	path := writeConfig(t, `
server:
  trusted_proxies:
    - definitely-not-a-network
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid trusted proxy to fail")
	}
}
