package config

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultTelemetryServiceName = "gemgate"
	DefaultTelemetrySampleRatio = 0.10
)

type TelemetryConfig struct {
	Enabled           bool    `yaml:"enabled,omitempty"`
	ServiceName       string  `yaml:"service_name,omitempty"`
	Endpoint          string  `yaml:"endpoint,omitempty"`
	SampleRatio       float64 `yaml:"sample_ratio,omitempty"`
	Environment       string  `yaml:"environment,omitempty"`
	PropagateUpstream bool    `yaml:"propagate_upstream,omitempty"`
}

type telemetryWire struct {
	Enabled           bool     `yaml:"enabled,omitempty"`
	ServiceName       string   `yaml:"service_name,omitempty"`
	Endpoint          string   `yaml:"endpoint,omitempty"`
	SampleRatio       *float64 `yaml:"sample_ratio,omitempty"`
	Environment       string   `yaml:"environment,omitempty"`
	PropagateUpstream bool     `yaml:"propagate_upstream,omitempty"`
}

func (c *TelemetryConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("telemetry must be a mapping")
	}
	allowed := map[string]struct{}{
		"enabled":            {},
		"service_name":       {},
		"endpoint":           {},
		"sample_ratio":       {},
		"environment":        {},
		"propagate_upstream": {},
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("telemetry contains unknown field %q", key)
		}
	}

	var raw telemetryWire
	if err := node.Decode(&raw); err != nil {
		return err
	}
	c.Enabled = raw.Enabled
	c.ServiceName = strings.TrimSpace(raw.ServiceName)
	if c.ServiceName == "" {
		c.ServiceName = DefaultTelemetryServiceName
	}
	c.Endpoint = strings.TrimSpace(raw.Endpoint)
	c.Environment = strings.TrimSpace(raw.Environment)
	c.PropagateUpstream = raw.PropagateUpstream
	c.SampleRatio = DefaultTelemetrySampleRatio
	if raw.SampleRatio != nil {
		c.SampleRatio = *raw.SampleRatio
	}
	return c.Validate()
}

func (c TelemetryConfig) Normalized() (TelemetryConfig, error) {
	c.ServiceName = strings.TrimSpace(c.ServiceName)
	if c.ServiceName == "" {
		c.ServiceName = DefaultTelemetryServiceName
	}
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Environment = strings.TrimSpace(c.Environment)
	if c.SampleRatio == 0 {
		c.SampleRatio = DefaultTelemetrySampleRatio
	}
	return c, c.Validate()
}

func (c TelemetryConfig) Validate() error {
	if c.SampleRatio < 0 || c.SampleRatio > 1 {
		return fmt.Errorf("telemetry.sample_ratio must be between 0 and 1")
	}
	if !c.Enabled {
		if c.PropagateUpstream {
			return fmt.Errorf("telemetry.propagate_upstream requires telemetry.enabled: true")
		}
		return nil
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return fmt.Errorf("telemetry.service_name must not be empty when telemetry is enabled")
	}
	if c.SampleRatio <= 0 {
		return fmt.Errorf("telemetry.sample_ratio must be greater than 0 when telemetry is enabled")
	}
	if c.Endpoint == "" {
		return nil // standard OTEL_EXPORTER_OTLP*_ENDPOINT environment variables may supply it
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("telemetry.endpoint must be an absolute http(s) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("telemetry.endpoint must not contain userinfo, query, or fragment")
	}
	return nil
}
