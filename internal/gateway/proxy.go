package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var errBodyTooLarge = errors.New("request body too large")

func (g *Gateway) proxy(state runtimeSnapshot, w http.ResponseWriter, r *http.Request, clientName, reqID string) (proxyResult, error) {
	p, targetPath, status, err := resolveProvider(state, r.URL.Path)
	if err != nil {
		return proxyResult{status: status}, err
	}
	result := proxyResult{provider: p.name}

	body, contentLength, err := prepareBody(state, r)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			result.status = http.StatusRequestEntityTooLarge
		} else {
			result.status = http.StatusBadRequest
		}
		return result, err
	}
	if contentLength > 0 {
		g.metrics.BytesIn.Add(uint64(contentLength))
	}

	upstreamURL := *p.baseURL
	upstreamURL.Path = singleJoiningSlash(p.baseURL.Path, strings.TrimPrefix(targetPath, "/"))
	upstreamURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(tagDownstreamContext(r.Context()), r.Method, upstreamURL.String(), body)
	if err != nil {
		result.status = http.StatusInternalServerError
		return result, fmt.Errorf("build upstream request: %w", err)
	}
	req.ContentLength = contentLength
	copyRequestHeaders(req.Header, r.Header)
	clientIP := resolveClientIP(state.cfg, r)
	if clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
		req.Header.Set("X-Real-IP", clientIP)
	}
	req.Header.Set("X-Request-ID", reqID)
	req.Header.Set("X-GemGate-Client", clientName)
	req.Header.Set("X-GemGate-Provider", p.name)
	req.Header.Set("Via", "1.1 gemgate")
	if err := p.spec.ApplyHeaders(req, p.apiKey, p.headers); err != nil {
		result.status = http.StatusInternalServerError
		return result, fmt.Errorf("apply provider %q auth: %w", p.name, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		result.status = http.StatusBadGateway
		return result, fmt.Errorf("provider %q request failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	result.status = resp.StatusCode
	result.responseStarted = true

	result.bytesOut, err = copyAndFlush(w, resp.Body)
	if result.bytesOut > 0 {
		g.metrics.BytesOut.Add(uint64(result.bytesOut))
	}
	if err != nil {
		if r.Context().Err() != nil {
			return result, r.Context().Err()
		}
		if !errors.Is(err, context.Canceled) {
			return result, fmt.Errorf("stream provider %q response: %w", p.name, err)
		}
		return result, err
	}
	if r.Context().Err() != nil {
		return result, r.Context().Err()
	}
	return result, nil
}

func resolveProvider(state runtimeSnapshot, path string) (*providerRuntime, string, int, error) {
	const prefix = "/providers/"
	if !strings.HasPrefix(path, prefix) {
		return state.defaultProvider, path, 0, nil
	}
	rest := strings.TrimPrefix(path, prefix)
	idx := strings.IndexByte(rest, '/')
	if idx <= 0 {
		return nil, "", http.StatusBadRequest, errors.New("provider route must be /providers/{name}/{path}")
	}
	name := rest[:idx]
	p := state.providers[name]
	if p == nil {
		return nil, "", http.StatusNotFound, fmt.Errorf("unknown provider %q", name)
	}
	return p, "/" + rest[idx+1:], 0, nil
}

func prepareBody(state runtimeSnapshot, r *http.Request) (io.Reader, int64, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, 0, nil
	}
	limit := state.cfg.RequestBodyLimit
	if limit <= 0 {
		return r.Body, r.ContentLength, nil
	}
	if r.ContentLength > limit {
		return nil, 0, fmt.Errorf("%w: configured limit is %d bytes", errBodyTooLarge, limit)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read request body: %w", err)
	}
	if int64(len(b)) > limit {
		return nil, 0, fmt.Errorf("%w: configured limit is %d bytes", errBodyTooLarge, limit)
	}
	return bytes.NewReader(b), int64(len(b)), nil
}

func authenticate(state runtimeSnapshot, r *http.Request) (clientAuth, string, bool) {
	token := bearerToken(r)
	if token == "" {
		return clientAuth{}, "", false
	}
	info, ok := state.tokens[token]
	return info, token, ok
}

func safeConfig(state runtimeSnapshot) map[string]any {
	clients := make([]map[string]any, 0, len(state.cfg.Config.Clients))
	for _, c := range state.cfg.Config.Clients {
		clients = append(clients, map[string]any{"name": c.Name, "enabled": c.Enabled, "token": redact(c.Token), "rate_limit_rpm": c.RateLimitRPM})
	}
	providers := make([]map[string]any, 0, len(state.cfg.Config.Providers))
	for _, p := range state.cfg.Config.Providers {
		policy := circuitPolicyFor(state.cfg, p.Name)
		providers = append(providers, map[string]any{
			"name": p.Name, "type": p.Type, "base_url": p.BaseURL, "api_key": redact(p.APIKey), "timeout": p.Timeout,
			"circuit_breaker": map[string]any{
				"enabled":           policy.enabled,
				"failure_threshold": policy.failureThreshold,
				"open_for":          policy.openFor.String(),
			},
		})
	}
	redisConfigured := strings.TrimSpace(state.cfg.Config.RateLimit.Redis.URL) != ""
	redisMode := ""
	if state.cfg.Config.RateLimit.Backend == "redis" && redisConfigured {
		redisMode = redisRateLimitMode(state.cfg.Config.RateLimit.Redis.URL)
	}
	telemetry := state.cfg.Config.Telemetry
	return map[string]any{
		"server": map[string]any{
			"listen":             state.cfg.Config.Server.Listen,
			"public_health":      state.cfg.Config.Server.PublicHealth,
			"request_body_limit": state.cfg.Config.Server.RequestBodyLimit,
			"trusted_proxies":    append([]string(nil), state.cfg.Config.Server.TrustedProxies...),
			"cors": map[string]any{
				"enabled":           state.cfg.Config.Server.CORS.IsEnabled(),
				"allowed_origins":   append([]string(nil), state.cfg.Config.Server.CORS.AllowedOrigins...),
				"allowed_methods":   append([]string(nil), state.cfg.Config.Server.CORS.AllowedMethods...),
				"allowed_headers":   append([]string(nil), state.cfg.Config.Server.CORS.AllowedHeaders...),
				"allow_credentials": state.cfg.Config.Server.CORS.AllowCredentials,
				"max_age":           state.cfg.Config.Server.CORS.MaxAge,
			},
		},
		"operations": map[string]any{
			"dedicated_auth": state.operationsToken != "",
		},
		"rate_limit": map[string]any{
			"backend":   state.cfg.Config.RateLimit.Backend,
			"fail_open": state.cfg.Config.RateLimit.Redis.FailOpen,
			"redis": map[string]any{
				"configured": redisConfigured,
				"mode":       redisMode,
				"key_prefix": state.cfg.Config.RateLimit.Redis.KeyPrefix,
				"timeout":    state.cfg.Config.RateLimit.Redis.Timeout,
			},
		},
		"telemetry": map[string]any{
			"enabled":             telemetry.Enabled,
			"service_name":        telemetry.ServiceName,
			"sample_ratio":        telemetry.SampleRatio,
			"environment":         telemetry.Environment,
			"propagate_upstream":  telemetry.PropagateUpstream,
			"endpoint_configured": strings.TrimSpace(telemetry.Endpoint) != "",
		},
		"default_provider": state.cfg.Config.DefaultProvider,
		"providers":        providers,
		"clients":          clients,
	}
}

func activeProviderMetrics(all []ProviderMetricsSnapshot, providers map[string]*providerRuntime) []ProviderMetricsSnapshot {
	out := make([]ProviderMetricsSnapshot, 0, len(providers))
	for _, snapshot := range all {
		if _, ok := providers[snapshot.Name]; ok {
			out = append(out, snapshot)
		}
	}
	return out
}
