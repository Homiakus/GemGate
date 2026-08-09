package config

import (
	"strings"
	"testing"
)

func TestLoggingRejectsSensitiveCaptureModes(t *testing.T) {
	for _, field := range []string{"log_body", "log_headers"} {
		t.Run(field, func(t *testing.T) {
			path := writeConfig(t, `
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
logging:
  recent: 100
  `+field+`: true
`)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected %s=true to be rejected", field)
			}
			if !strings.Contains(err.Error(), "intentionally unsupported") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoggingDisabledModesRemainCompatible(t *testing.T) {
	path := writeConfig(t, `
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
logging:
  recent: 123
  log_body: false
  log_headers: false
`)
	rt, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Config.Logging.Recent != 123 {
		t.Fatalf("recent = %d", rt.Config.Logging.Recent)
	}
}
