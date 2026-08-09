package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsNegativeClientRateLimit(t *testing.T) {
	rt := Runtime{
		Config: Config{
			Providers:       []ProviderConfig{{Name: "gemini", Type: "gemini", BaseURL: "https://example.test", APIKey: "gemini-key"}},
			DefaultProvider: "gemini",
			Clients:         []ClientConfig{{Name: "local-dev", Token: "client-token", Enabled: true, RateLimitRPM: -1}},
		},
	}
	if err := validate(rt); err == nil {
		t.Fatal("expected negative rate_limit_rpm to be rejected")
	}
}

func TestLegacyUpstreamBecomesGeminiProvider(t *testing.T) {
	path := writeConfig(t, `
upstream:
  api_key: "legacy-key"
clients:
  - name: local
    token: token
    enabled: true
`)
	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Config.DefaultProvider; got != "gemini" {
		t.Fatalf("default provider = %q", got)
	}
	if len(rt.Config.Providers) != 1 || rt.Config.Providers[0].Type != "gemini" {
		t.Fatalf("providers = %#v", rt.Config.Providers)
	}
}

func TestProviderDefaultsAndStrictYAML(t *testing.T) {
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
	if got := rt.Config.Providers[0].BaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("base URL = %q", got)
	}

	bad := writeConfig(t, `
server:
  lsten: ":8080"
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(bad); err == nil {
		t.Fatal("expected unknown YAML field to fail")
	}
}

func TestRejectsDuplicateProviders(t *testing.T) {
	path := writeConfig(t, `
default_provider: same
providers:
  - name: same
    type: openai
    api_key: a
  - name: same
    type: groq
    api_key: b
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected duplicate provider names to fail")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
