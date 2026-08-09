package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gemgate/internal/provider"

	"gopkg.in/yaml.v3"
)

const (
	DefaultCircuitFailureThreshold = 5
	DefaultCircuitOpenFor          = 30 * time.Second
	DefaultRateLimitBackend        = "memory"
	DefaultRedisRateLimitPrefix    = "gemgate:ratelimit:"
	DefaultRedisRateLimitTimeout   = 2 * time.Second
)

type Config struct {
	Server          ServerConfig     `yaml:"server"`
	Upstream        UpstreamConfig   `yaml:"upstream,omitempty"` // legacy single-provider config
	Providers       []ProviderConfig `yaml:"providers,omitempty"`
	DefaultProvider string           `yaml:"default_provider,omitempty"`
	Operations      OperationsConfig `yaml:"operations,omitempty"`
	Clients         []ClientConfig   `yaml:"clients"`
	RateLimit       RateLimitConfig  `yaml:"rate_limit,omitempty"`
	Logging         LoggingConfig    `yaml:"logging"`
}

type ServerConfig struct {
	Listen           string     `yaml:"listen"`
	ReadTimeout      string     `yaml:"read_timeout"`
	WriteTimeout     string     `yaml:"write_timeout"`
	IdleTimeout      string     `yaml:"idle_timeout"`
	PublicHealth     bool       `yaml:"public_health"`
	RequestBodyLimit string     `yaml:"request_body_limit"`
	TrustedProxies   []string   `yaml:"trusted_proxies,omitempty"`
	CORS             CORSConfig `yaml:"cors,omitempty"`
}

type CORSConfig struct {
	Enabled          *bool    `yaml:"enabled,omitempty"`
	AllowedOrigins   []string `yaml:"allowed_origins,omitempty"`
	AllowedMethods   []string `yaml:"allowed_methods,omitempty"`
	AllowedHeaders   []string `yaml:"allowed_headers,omitempty"`
	AllowCredentials bool     `yaml:"allow_credentials,omitempty"`
	MaxAge           string   `yaml:"max_age,omitempty"`
}

func (c CORSConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

type CircuitBreakerConfig struct {
	Enabled          *bool  `yaml:"enabled,omitempty"`
	FailureThreshold int    `yaml:"failure_threshold,omitempty"`
	OpenFor          string `yaml:"open_for,omitempty"`
}

func (c CircuitBreakerConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

type CircuitBreakerRuntime struct {
	Enabled          bool
	FailureThreshold int
	OpenFor          time.Duration
}

type OperationsConfig struct {
	Token     string `yaml:"token,omitempty"`
	TokenFile string `yaml:"token_file,omitempty"`
}

type RateLimitConfig struct {
	Backend string               `yaml:"backend,omitempty"`
	Redis   RedisRateLimitConfig `yaml:"redis,omitempty"`
}

type RedisRateLimitConfig struct {
	URL       string `yaml:"url,omitempty"`
	URLFile   string `yaml:"url_file,omitempty"`
	KeyPrefix string `yaml:"key_prefix,omitempty"`
	Timeout   string `yaml:"timeout,omitempty"`
	FailOpen  bool   `yaml:"fail_open,omitempty"`
}

type UpstreamConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	APIKeyFile string `yaml:"api_key_file,omitempty"`
	Timeout    string `yaml:"timeout"`
}

type ProviderConfig struct {
	Name           string               `yaml:"name"`
	Type           string               `yaml:"type"`
	BaseURL        string               `yaml:"base_url,omitempty"`
	APIKey         string               `yaml:"api_key,omitempty"`
	APIKeyFile     string               `yaml:"api_key_file,omitempty"`
	Timeout        string               `yaml:"timeout,omitempty"`
	Headers        map[string]string    `yaml:"headers,omitempty"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker,omitempty"`
}

type ClientConfig struct {
	Name         string `yaml:"name"`
	Token        string `yaml:"token,omitempty"`
	TokenFile    string `yaml:"token_file,omitempty"`
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
	UpstreamTimeout  time.Duration // default provider timeout, kept for compatibility
	ProviderTimeouts map[string]time.Duration
	ProviderCircuits map[string]CircuitBreakerRuntime
	TrustedProxies   []netip.Prefix
	RequestBodyLimit int64
	CORSMaxAge       time.Duration
	RateLimitTimeout time.Duration
}

var (
	envPattern          = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	providerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

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
	decoder := yaml.NewDecoder(strings.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Runtime{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	if err := resolveSecretFiles(&cfg, filepath.Dir(path)); err != nil {
		return Runtime{}, err
	}
	syncLegacyUpstream(&cfg)

	rt := Runtime{
		Config:           cfg,
		ProviderTimeouts: make(map[string]time.Duration, len(cfg.Providers)),
		ProviderCircuits: make(map[string]CircuitBreakerRuntime, len(cfg.Providers)),
	}
	if rt.ReadTimeout, err = parseDuration(cfg.Server.ReadTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.read_timeout: %w", err)
	}
	if rt.WriteTimeout, err = parseDuration(cfg.Server.WriteTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.write_timeout: %w", err)
	}
	if rt.IdleTimeout, err = parseDuration(cfg.Server.IdleTimeout); err != nil {
		return Runtime{}, fmt.Errorf("server.idle_timeout: %w", err)
	}
	if rt.RequestBodyLimit, err = parseBytes(cfg.Server.RequestBodyLimit); err != nil {
		return Runtime{}, fmt.Errorf("server.request_body_limit: %w", err)
	}
	if rt.CORSMaxAge, err = parseDuration(cfg.Server.CORS.MaxAge); err != nil {
		return Runtime{}, fmt.Errorf("server.cors.max_age: %w", err)
	}
	if rt.TrustedProxies, err = parseTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		return Runtime{}, fmt.Errorf("server.trusted_proxies: %w", err)
	}
	if rt.RateLimitTimeout, err = parseDuration(cfg.RateLimit.Redis.Timeout); err != nil {
		return Runtime{}, fmt.Errorf("rate_limit.redis.timeout: %w", err)
	}
	for _, p := range cfg.Providers {
		d, parseErr := parseDuration(p.Timeout)
		if parseErr != nil {
			return Runtime{}, fmt.Errorf("provider %q timeout: %w", p.Name, parseErr)
		}
		rt.ProviderTimeouts[p.Name] = d
		if p.Name == cfg.DefaultProvider {
			rt.UpstreamTimeout = d
		}
		openFor, parseErr := parseDuration(p.CircuitBreaker.OpenFor)
		if parseErr != nil {
			return Runtime{}, fmt.Errorf("provider %q circuit_breaker.open_for: %w", p.Name, parseErr)
		}
		rt.ProviderCircuits[p.Name] = CircuitBreakerRuntime{
			Enabled:          p.CircuitBreaker.IsEnabled(),
			FailureThreshold: p.CircuitBreaker.FailureThreshold,
			OpenFor:          openFor,
		}
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
	for i := range cfg.Server.TrustedProxies {
		cfg.Server.TrustedProxies[i] = strings.TrimSpace(cfg.Server.TrustedProxies[i])
	}
	if cfg.Server.CORS.Enabled == nil {
		enabled := true
		cfg.Server.CORS.Enabled = &enabled
	}
	if len(cfg.Server.CORS.AllowedOrigins) == 0 {
		cfg.Server.CORS.AllowedOrigins = []string{"*"}
	}
	if len(cfg.Server.CORS.AllowedMethods) == 0 {
		cfg.Server.CORS.AllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(cfg.Server.CORS.AllowedHeaders) == 0 {
		cfg.Server.CORS.AllowedHeaders = []string{"Authorization", "Content-Type", "X-Request-ID"}
	}
	if cfg.Server.CORS.MaxAge == "" {
		cfg.Server.CORS.MaxAge = "10m"
	}
	for i := range cfg.Server.CORS.AllowedOrigins {
		origin := strings.TrimSpace(cfg.Server.CORS.AllowedOrigins[i])
		if origin != "*" {
			origin = strings.TrimSuffix(origin, "/")
		}
		cfg.Server.CORS.AllowedOrigins[i] = origin
	}
	for i := range cfg.Server.CORS.AllowedMethods {
		cfg.Server.CORS.AllowedMethods[i] = strings.ToUpper(strings.TrimSpace(cfg.Server.CORS.AllowedMethods[i]))
	}
	for i := range cfg.Server.CORS.AllowedHeaders {
		cfg.Server.CORS.AllowedHeaders[i] = strings.TrimSpace(cfg.Server.CORS.AllowedHeaders[i])
	}
	if cfg.Logging.Recent <= 0 {
		cfg.Logging.Recent = 300
	}

	cfg.Operations.Token = strings.TrimSpace(cfg.Operations.Token)
	cfg.Operations.TokenFile = strings.TrimSpace(cfg.Operations.TokenFile)

	cfg.RateLimit.Backend = strings.ToLower(strings.TrimSpace(cfg.RateLimit.Backend))
	if cfg.RateLimit.Backend == "" {
		cfg.RateLimit.Backend = DefaultRateLimitBackend
	}
	cfg.RateLimit.Redis.URL = strings.TrimSpace(cfg.RateLimit.Redis.URL)
	cfg.RateLimit.Redis.URLFile = strings.TrimSpace(cfg.RateLimit.Redis.URLFile)
	cfg.RateLimit.Redis.KeyPrefix = strings.TrimSpace(cfg.RateLimit.Redis.KeyPrefix)
	if cfg.RateLimit.Redis.KeyPrefix == "" {
		cfg.RateLimit.Redis.KeyPrefix = DefaultRedisRateLimitPrefix
	}
	if cfg.RateLimit.Redis.Timeout == "" {
		cfg.RateLimit.Redis.Timeout = DefaultRedisRateLimitTimeout.String()
	}

	if len(cfg.Providers) == 0 {
		if cfg.Upstream.BaseURL == "" {
			cfg.Upstream.BaseURL = "https://generativelanguage.googleapis.com"
		}
		if cfg.Upstream.Timeout == "" {
			cfg.Upstream.Timeout = "0s"
		}
		cfg.Providers = []ProviderConfig{{
			Name:       "gemini",
			Type:       "gemini",
			BaseURL:    cfg.Upstream.BaseURL,
			APIKey:     cfg.Upstream.APIKey,
			APIKeyFile: cfg.Upstream.APIKeyFile,
			Timeout:    cfg.Upstream.Timeout,
		}}
		if cfg.DefaultProvider == "" {
			cfg.DefaultProvider = "gemini"
		}
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		p.Name = strings.TrimSpace(p.Name)
		p.Type = provider.NormalizeType(p.Type)
		p.APIKeyFile = strings.TrimSpace(p.APIKeyFile)
		if p.Type == "" {
			p.Type = "openai-compatible"
		}
		if spec, ok := provider.Lookup(p.Type); ok && p.BaseURL == "" {
			p.BaseURL = spec.DefaultBaseURL
		}
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if p.Timeout == "" {
			p.Timeout = "0s"
		}
		if p.CircuitBreaker.Enabled == nil {
			enabled := true
			p.CircuitBreaker.Enabled = &enabled
		}
		if p.CircuitBreaker.FailureThreshold == 0 {
			p.CircuitBreaker.FailureThreshold = DefaultCircuitFailureThreshold
		}
		if p.CircuitBreaker.OpenFor == "" {
			p.CircuitBreaker.OpenFor = DefaultCircuitOpenFor.String()
		}
	}
	for i := range cfg.Clients {
		cfg.Clients[i].TokenFile = strings.TrimSpace(cfg.Clients[i].TokenFile)
	}
	if cfg.DefaultProvider == "" && len(cfg.Providers) > 0 {
		cfg.DefaultProvider = cfg.Providers[0].Name
	}
	syncLegacyUpstream(cfg)
}

func resolveSecretFiles(cfg *Config, baseDir string) error {
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if strings.TrimSpace(p.APIKey) != "" && p.APIKeyFile != "" {
			return fmt.Errorf("provider %q: api_key and api_key_file are mutually exclusive", p.Name)
		}
		if p.APIKeyFile == "" {
			continue
		}
		secret, err := readSecretFile(baseDir, p.APIKeyFile)
		if err != nil {
			return fmt.Errorf("provider %q api_key_file: %w", p.Name, err)
		}
		p.APIKey = secret
	}
	for i := range cfg.Clients {
		c := &cfg.Clients[i]
		if strings.TrimSpace(c.Token) != "" && c.TokenFile != "" {
			return fmt.Errorf("client %q: token and token_file are mutually exclusive", c.Name)
		}
		if c.TokenFile == "" {
			continue
		}
		secret, err := readSecretFile(baseDir, c.TokenFile)
		if err != nil {
			return fmt.Errorf("client %q token_file: %w", c.Name, err)
		}
		c.Token = secret
	}
	if cfg.Operations.Token != "" && cfg.Operations.TokenFile != "" {
		return errors.New("operations.token and token_file are mutually exclusive")
	}
	if cfg.Operations.TokenFile != "" {
		secret, err := readSecretFile(baseDir, cfg.Operations.TokenFile)
		if err != nil {
			return fmt.Errorf("operations.token_file: %w", err)
		}
		cfg.Operations.Token = secret
	}
	redisCfg := &cfg.RateLimit.Redis
	if redisCfg.URL != "" && redisCfg.URLFile != "" {
		return errors.New("rate_limit.redis.url and url_file are mutually exclusive")
	}
	if redisCfg.URLFile != "" {
		secret, err := readSecretFile(baseDir, redisCfg.URLFile)
		if err != nil {
			return fmt.Errorf("rate_limit.redis.url_file: %w", err)
		}
		redisCfg.URL = secret
	}
	return nil
}

func readSecretFile(baseDir, name string) (string, error) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(b))
	if secret == "" {
		return "", errors.New("secret file is empty")
	}
	return secret, nil
}

func syncLegacyUpstream(cfg *Config) {
	for _, p := range cfg.Providers {
		if p.Name == cfg.DefaultProvider {
			cfg.Upstream = UpstreamConfig{BaseURL: p.BaseURL, APIKey: p.APIKey, APIKeyFile: p.APIKeyFile, Timeout: p.Timeout}
			return
		}
	}
}

func validate(rt Runtime) error {
	cfg := rt.Config
	if len(cfg.Providers) == 0 {
		return errors.New("no providers configured")
	}
	seenProviders := make(map[string]struct{}, len(cfg.Providers))
	defaultFound := false
	for _, p := range cfg.Providers {
		if p.Name == "" {
			return errors.New("provider has empty name")
		}
		if !providerNamePattern.MatchString(p.Name) {
			return fmt.Errorf("provider name %q contains unsupported characters", p.Name)
		}
		if _, exists := seenProviders[p.Name]; exists {
			return fmt.Errorf("duplicate provider name %q", p.Name)
		}
		seenProviders[p.Name] = struct{}{}
		if p.Name == cfg.DefaultProvider {
			defaultFound = true
		}
		spec, ok := provider.Lookup(p.Type)
		if !ok {
			return fmt.Errorf("provider %q has unsupported type %q", p.Name, p.Type)
		}
		if spec.RequiresAPIKey && strings.TrimSpace(p.APIKey) == "" {
			return fmt.Errorf("provider %q (%s) requires api_key or api_key_file", p.Name, p.Type)
		}
		if strings.TrimSpace(p.BaseURL) == "" {
			return fmt.Errorf("provider %q requires base_url", p.Name)
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("provider %q base_url must be an absolute http(s) URL", p.Name)
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("provider %q base_url must not contain userinfo, query, or fragment", p.Name)
		}
		for key := range p.Headers {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("provider %q has empty header name", p.Name)
			}
		}
		policy, ok := rt.ProviderCircuits[p.Name]
		if !ok {
			policy = CircuitBreakerRuntime{Enabled: true, FailureThreshold: DefaultCircuitFailureThreshold, OpenFor: DefaultCircuitOpenFor}
		}
		if policy.Enabled {
			if policy.FailureThreshold <= 0 {
				return fmt.Errorf("provider %q circuit_breaker.failure_threshold must be positive", p.Name)
			}
			if policy.OpenFor <= 0 {
				return fmt.Errorf("provider %q circuit_breaker.open_for must be positive", p.Name)
			}
		}
	}
	if !defaultFound {
		return fmt.Errorf("default_provider %q does not match a configured provider", cfg.DefaultProvider)
	}

	activeClients := 0
	seenTokens := map[string]struct{}{}
	for _, c := range cfg.Clients {
		if !c.Enabled {
			continue
		}
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("enabled client has empty name")
		}
		if strings.TrimSpace(c.Token) == "" {
			return fmt.Errorf("client %q has empty token; set token or token_file", c.Name)
		}
		if _, ok := seenTokens[c.Token]; ok {
			return fmt.Errorf("duplicate client token for %q", c.Name)
		}
		if c.RateLimitRPM < 0 {
			return fmt.Errorf("client %q rate_limit_rpm must not be negative", c.Name)
		}
		seenTokens[c.Token] = struct{}{}
		activeClients++
	}
	if activeClients == 0 {
		return errors.New("no enabled clients; add at least one clients[].token or token_file")
	}
	if cfg.Operations.Token != "" {
		if _, duplicated := seenTokens[cfg.Operations.Token]; duplicated {
			return errors.New("operations token must be distinct from every enabled client token")
		}
	}
	if rt.RequestBodyLimit < 0 {
		return errors.New("request_body_limit must not be negative")
	}
	if err := validateCORS(cfg.Server.CORS, rt.CORSMaxAge); err != nil {
		return err
	}
	if err := validateRateLimit(cfg.RateLimit, rt.RateLimitTimeout); err != nil {
		return err
	}
	return nil
}

func validateRateLimit(cfg RateLimitConfig, timeout time.Duration) error {
	switch cfg.Backend {
	case "memory":
		return nil
	case "redis":
	default:
		return fmt.Errorf("rate_limit.backend must be memory or redis, got %q", cfg.Backend)
	}
	if strings.TrimSpace(cfg.Redis.URL) == "" {
		return errors.New("rate_limit.redis.url or url_file is required when backend is redis")
	}
	u, err := url.Parse(cfg.Redis.URL)
	if err != nil || u.Host == "" || (u.Scheme != "redis" && u.Scheme != "rediss") {
		return errors.New("rate_limit.redis.url must be an absolute redis:// or rediss:// URL")
	}
	if u.Fragment != "" {
		return errors.New("rate_limit.redis.url must not contain a fragment")
	}
	if timeout <= 0 {
		return errors.New("rate_limit.redis.timeout must be positive")
	}
	prefix := cfg.Redis.KeyPrefix
	if strings.TrimSpace(prefix) == "" {
		return errors.New("rate_limit.redis.key_prefix must not be empty")
	}
	if len(prefix) > 128 {
		return errors.New("rate_limit.redis.key_prefix must not exceed 128 bytes")
	}
	for _, r := range prefix {
		if r < 0x20 || r == 0x7f {
			return errors.New("rate_limit.redis.key_prefix contains control characters")
		}
	}
	return nil
}

func validateCORS(cfg CORSConfig, maxAge time.Duration) error {
	if !cfg.IsEnabled() {
		return nil
	}
	if len(cfg.AllowedOrigins) == 0 {
		return errors.New("server.cors.allowed_origins must not be empty when CORS is enabled")
	}
	wildcard := false
	for _, origin := range cfg.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			return errors.New("server.cors.allowed_origins contains an empty origin")
		}
		if origin == "*" {
			wildcard = true
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("server.cors.allowed_origins contains invalid origin %q", origin)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("server.cors.allowed_origins origin %q must use http or https", origin)
		}
		if u.Path != "" && u.Path != "/" {
			return fmt.Errorf("server.cors.allowed_origins origin %q must not contain a path", origin)
		}
	}
	if wildcard && cfg.AllowCredentials {
		return errors.New("server.cors.allow_credentials cannot be true with wildcard allowed_origins")
	}
	if len(cfg.AllowedMethods) == 0 {
		return errors.New("server.cors.allowed_methods must not be empty when CORS is enabled")
	}
	for _, method := range cfg.AllowedMethods {
		if strings.TrimSpace(method) == "" {
			return errors.New("server.cors.allowed_methods contains an empty method")
		}
	}
	if len(cfg.AllowedHeaders) == 0 {
		return errors.New("server.cors.allowed_headers must not be empty when CORS is enabled")
	}
	for _, header := range cfg.AllowedHeaders {
		if strings.TrimSpace(header) == "" {
			return errors.New("server.cors.allowed_headers contains an empty header")
		}
	}
	if maxAge < 0 {
		return errors.New("server.cors.max_age must not be negative")
	}
	return nil
}

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))
	seen := make(map[netip.Prefix]struct{}, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, errors.New("contains an empty entry")
		}
		var prefix netip.Prefix
		if strings.Contains(raw, "/") {
			parsed, err := netip.ParsePrefix(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", raw, err)
			}
			prefix = parsed.Masked()
		} else {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid IP %q: %w", raw, err)
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if prefix.Addr().Is4In6() {
			addr := prefix.Addr().Unmap()
			bits := prefix.Bits() - 96
			if bits < 0 {
				bits = 0
			}
			prefix = netip.PrefixFrom(addr, bits).Masked()
		}
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	return out, nil
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
	}{{"KiB", 1024}, {"MiB", 1024 * 1024}, {"GiB", 1024 * 1024 * 1024}, {"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numeric := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			n, err := strconv.ParseInt(numeric, 10, 64)
			if err != nil {
				return 0, err
			}
			if n < 0 {
				return 0, errors.New("size must not be negative")
			}
			if n > (1<<63-1)/u.mult {
				return 0, errors.New("size overflows int64")
			}
			return n * u.mult, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("size must not be negative")
	}
	return n, nil
}
