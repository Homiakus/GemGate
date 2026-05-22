package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Clients  []ClientConfig `yaml:"clients"`
	Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
	Listen           string `yaml:"listen"`
	ReadTimeout      string `yaml:"read_timeout"`
	WriteTimeout     string `yaml:"write_timeout"`
	IdleTimeout      string `yaml:"idle_timeout"`
	PublicHealth     bool   `yaml:"public_health"`
	RequestBodyLimit string `yaml:"request_body_limit"`
}

type UpstreamConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Timeout string `yaml:"timeout"`
}

type ClientConfig struct {
	Name         string `yaml:"name"`
	Token        string `yaml:"token"`
	Enabled      bool   `yaml:"enabled"`
	RateLimitRPM int    `yaml:"rate_limit_rpm"`
}

type LoggingConfig struct {
	Recent     int  `yaml:"recent"`
	LogBody    bool `yaml:"log_body"`
	LogHeaders bool `yaml:"log_headers"`
}

type Runtime struct {
	Config           Config
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	UpstreamTimeout  time.Duration
	RequestBodyLimit int64
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func Load(path string) (Runtime, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Runtime{}, fmt.Errorf("read config: %w", err)
	}

	expanded := envPattern.ReplaceAllStringFunc(string(b), func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		return os.Getenv(name)
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return Runtime{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)

	rt := Runtime{Config: cfg}
	if rt.ReadTimeout, err = parseDuration(cfg.Server.ReadTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.read_timeout: %w", err)
	}
	if rt.WriteTimeout, err = parseDuration(cfg.Server.WriteTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.write_timeout: %w", err)
	}
	if rt.IdleTimeout, err = parseDuration(cfg.Server.IdleTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.idle_timeout: %w", err)
	}
	if rt.UpstreamTimeout, err = parseDuration(cfg.Upstream.Timeout); err != nil {
		return Runtime{}, fmt.Errorf("upstream.timeout: %w", err)
	}
	if rt.RequestBodyLimit, err = parseBytes(cfg.Server.RequestBodyLimit); err != nil {
		return Runtime{}, fmt.Errorf("server.request_body_limit: %w", err)
	}

	if err := validate(rt); err != nil {
		return Runtime{}, err
	}
	return rt, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Server.ReadTimeout == "" {
		cfg.Server.ReadTimeout = "30s"
	}
	if cfg.Server.WriteTimeout == "" {
		cfg.Server.WriteTimeout = "0s"
	}
	if cfg.Server.IdleTimeout == "" {
		cfg.Server.IdleTimeout = "120s"
	}
	if cfg.Server.RequestBodyLimit == "" {
		cfg.Server.RequestBodyLimit = "32MiB"
	}
	if cfg.Upstream.BaseURL == "" {
		cfg.Upstream.BaseURL = "https://generativelanguage.googleapis.com"
	}
	if cfg.Upstream.Timeout == "" {
		cfg.Upstream.Timeout = "0s"
	}
	if cfg.Logging.Recent <= 0 {
		cfg.Logging.Recent = 300
	}
}

func validate(rt Runtime) error {
	cfg := rt.Config
	if strings.TrimSpace(cfg.Upstream.APIKey) == "" {
		return errors.New("upstream.api_key is empty; set GEMINI_API_KEY or put a key in config")
	}
	activeClients := 0
	seen := map[string]struct{}{}
	for _, c := range cfg.Clients {
		if !c.Enabled {
			continue
		}
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("enabled client has empty name")
		}
		if strings.TrimSpace(c.Token) == "" {
			return fmt.Errorf("client %q has empty token", c.Name)
		}
		if _, ok := seen[c.Token]; ok {
			return fmt.Errorf("duplicate client token for %q", c.Name)
		}
		if c.RateLimitRPM < 0 {
			return fmt.Errorf("client %q rate_limit_rpm must not be negative", c.Name)
		}
		seen[c.Token] = struct{}{}
		activeClients++
	}
	if activeClients == 0 {
		return errors.New("no enabled clients; add at least one clients[].token")
	}
	if rt.RequestBodyLimit < 0 {
		return errors.New("request_body_limit must not be negative")
	}
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" || s == "0" || s == "0s" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"KiB", 1024}, {"MiB", 1024 * 1024}, {"GiB", 1024 * 1024 * 1024},
		{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			var n int64
			_, err := fmt.Sscanf(strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), "%d", &n)
			return n * u.mult, err
		}
	}
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
