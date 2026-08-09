package config

import "testing"

func TestTelemetryConfigDefaultsAndValidation(t *testing.T) {
	path := writeConfig(t, `
telemetry:
  enabled: true
  endpoint: "http://collector:4318/v1/traces"
  environment: production
upstream:
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
	cfg := rt.Config.Telemetry
	if !cfg.Enabled {
		t.Fatal("telemetry should be enabled")
	}
	if cfg.ServiceName != DefaultTelemetryServiceName {
		t.Fatalf("service name = %q", cfg.ServiceName)
	}
	if cfg.SampleRatio != DefaultTelemetrySampleRatio {
		t.Fatalf("sample ratio = %v", cfg.SampleRatio)
	}
	if cfg.Endpoint != "http://collector:4318/v1/traces" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Environment != "production" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
}

func TestTelemetryConfigRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `
telemetry:
  enabled: true
  endpont: "http://collector:4318/v1/traces"
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown telemetry field to fail")
	}
}

func TestTelemetryConfigRejectsInvalidSampleRatio(t *testing.T) {
	path := writeConfig(t, `
telemetry:
  enabled: true
  sample_ratio: 1.5
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid telemetry sample ratio to fail")
	}
}

func TestTelemetryConfigRejectsPropagationWhenDisabled(t *testing.T) {
	path := writeConfig(t, `
telemetry:
  enabled: false
  propagate_upstream: true
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected upstream propagation to require enabled telemetry")
	}
}

func TestTelemetryConfigRejectsCredentialBearingEndpoint(t *testing.T) {
	path := writeConfig(t, `
telemetry:
  enabled: true
  endpoint: "https://user:secret@collector.example/v1/traces"
upstream:
  api_key: secret
clients:
  - name: local
    token: token
    enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected telemetry endpoint userinfo to fail")
	}
}
