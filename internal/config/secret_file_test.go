package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsProviderAndClientSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "provider.key"), []byte("provider-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "client.token"), []byte("client-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	body := `
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key_file: provider.key
clients:
  - name: local
    token_file: client.token
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Config.Providers[0].APIKey; got != "provider-secret" {
		t.Fatalf("provider key = %q", got)
	}
	if got := rt.Config.Clients[0].Token; got != "client-secret" {
		t.Fatalf("client token = %q", got)
	}
}

func TestLoadRejectsInlineAndFileSecretTogether(t *testing.T) {
	path := writeConfig(t, `
default_provider: openai
providers:
  - name: openai
    type: openai
    api_key: inline
    api_key_file: provider.key
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected api_key/api_key_file conflict")
	}
}

func TestLegacyUpstreamSupportsAPIKeyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gemini.key"), []byte("gemini-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	body := `
upstream:
  api_key_file: gemini.key
clients:
  - name: local
    token: token
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Config.Providers[0].APIKey; got != "gemini-secret" {
		t.Fatalf("legacy provider key = %q", got)
	}
}
