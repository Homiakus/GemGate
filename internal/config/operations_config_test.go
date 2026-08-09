package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationsTokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ops-token"), []byte("operations-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	body := `
operations:
  token_file: ops-token
upstream:
  api_key: secret
clients:
  - name: app
    token: client-token
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	rt, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Config.Operations.Token != "operations-secret" {
		t.Fatalf("operations token = %q", rt.Config.Operations.Token)
	}
}

func TestOperationsTokenSourcesAreMutuallyExclusive(t *testing.T) {
	path := writeConfig(t, `
operations:
  token: direct
  token_file: ops-token
upstream:
  api_key: secret
clients:
  - name: app
    token: client-token
    enabled: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "operations.token and token_file are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOperationsTokenMustDifferFromClientTokens(t *testing.T) {
	path := writeConfig(t, `
operations:
  token: shared-token
upstream:
  api_key: secret
clients:
  - name: app
    token: shared-token
    enabled: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "operations token must be distinct") {
		t.Fatalf("unexpected error: %v", err)
	}
}
