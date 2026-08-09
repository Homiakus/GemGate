package config

import (
	"errors"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML keeps the legacy logging fields parseable while refusing modes
// that would expose request/response data. GemGate intentionally does not
// implement body/header capture until a field-level redaction contract exists.
func (c *LoggingConfig) UnmarshalYAML(value *yaml.Node) error {
	var decoded struct {
		Recent     int  `yaml:"recent"`
		LogBody    bool `yaml:"log_body"`
		LogHeaders bool `yaml:"log_headers"`
	}
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	if decoded.LogBody || decoded.LogHeaders {
		return errors.New("logging.log_body and logging.log_headers are intentionally unsupported; sensitive request/response capture must remain disabled")
	}
	*c = LoggingConfig{
		Recent:     decoded.Recent,
		LogBody:    false,
		LogHeaders: false,
	}
	return nil
}
