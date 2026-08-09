package config

import (
	"testing"
	"time"
)

func TestCircuitBreakerDefaults(t *testing.T) {
	path := writeConfig(t, `
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
	policy := rt.ProviderCircuits["openai"]
	if !policy.Enabled || policy.FailureThreshold != 5 || policy.OpenFor != 30*time.Second {
		t.Fatalf("unexpected circuit defaults: %#v", policy)
	}
}

func TestCircuitBreakerCustomAndDisabled(t *testing.T) {
	path := writeConfig(t, `
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key: secret
    circuit_breaker:
      enabled: true
      failure_threshold: 8
      open_for: 45s
  - name: local
    type: none
    base_url: http://127.0.0.1:11434
    circuit_breaker:
      enabled: false
clients:
  - name: local
    token: token
    enabled: true
`)
	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	openai := rt.ProviderCircuits["openai"]
	if !openai.Enabled || openai.FailureThreshold != 8 || openai.OpenFor != 45*time.Second {
		t.Fatalf("unexpected custom circuit: %#v", openai)
	}
	if rt.ProviderCircuits["local"].Enabled {
		t.Fatal("local circuit should be disabled")
	}
}

func TestCircuitBreakerRejectsInvalidEnabledPolicy(t *testing.T) {
	path := writeConfig(t, `
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key: secret
    circuit_breaker:
      failure_threshold: -1
      open_for: 0s
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid circuit policy to fail")
	}
}
