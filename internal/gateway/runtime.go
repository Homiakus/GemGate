package gateway

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"gemgate/internal/config"
	"gemgate/internal/provider"
)

type runtimeSnapshot struct {
	cfg             config.Runtime
	providers       map[string]*providerRuntime
	defaultProvider *providerRuntime
	tokens          map[string]clientAuth
	limits          map[string]*rateWindow
	cors            *corsHandler
}

type ReloadResult struct {
	Changed   bool
	Reloaded  time.Time
	Providers int
	Clients   int
}

func buildRuntimeSnapshot(g *Gateway, rt config.Runtime, previous *runtimeSnapshot) (runtimeSnapshot, error) {
	providers := make(map[string]*providerRuntime, len(rt.Config.Providers))
	now := time.Now()
	for _, p := range rt.Config.Providers {
		spec, ok := provider.Lookup(p.Type)
		if !ok {
			return runtimeSnapshot{}, fmt.Errorf("provider %q has unsupported type %q", p.Name, p.Type)
		}
		u, err := url.Parse(strings.TrimRight(p.BaseURL, "/"))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return runtimeSnapshot{}, fmt.Errorf("provider %q has invalid base_url", p.Name)
		}
		headers := make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			headers[k] = v
		}
		g.metrics.provider(p.Name)

		policy := circuitPolicyFor(rt, p.Name)
		breaker := newCircuitBreakerWithPolicy(policy)
		if previous != nil {
			if oldProvider := previous.providers[p.Name]; oldProvider != nil {
				breaker = oldProvider.breaker.cloneWithPolicy(policy, now)
			}
		}
		providers[p.Name] = &providerRuntime{
			name: p.Name, spec: spec, baseURL: u, apiKey: p.APIKey, headers: headers, breaker: breaker,
			client: &http.Client{
				Timeout:   rt.ProviderTimeouts[p.Name],
				Transport: newProviderMetricsTransport(p.Name, g.transport, g.metrics, breaker),
			},
		}
	}
	defaultProvider := providers[rt.Config.DefaultProvider]
	if defaultProvider == nil {
		return runtimeSnapshot{}, fmt.Errorf("default provider %q is not configured", rt.Config.DefaultProvider)
	}

	tokens := make(map[string]clientAuth)
	limits := make(map[string]*rateWindow)
	for _, c := range rt.Config.Clients {
		if !c.Enabled {
			continue
		}
		tokens[c.Token] = clientAuth{Name: c.Name, RateLimitRPM: c.RateLimitRPM}
		if c.RateLimitRPM <= 0 {
			continue
		}
		if previous != nil {
			if existing := previous.limits[c.Token]; existing != nil {
				limits[c.Token] = existing
				continue
			}
		}
		limits[c.Token] = &rateWindow{}
	}

	return runtimeSnapshot{
		cfg: rt, providers: providers, defaultProvider: defaultProvider,
		tokens: tokens, limits: limits,
		cors: newCORSPolicy(rt.Config.Server.CORS, rt.CORSMaxAge),
	}, nil
}

func circuitPolicyFor(rt config.Runtime, providerName string) circuitPolicy {
	configured, ok := rt.ProviderCircuits[providerName]
	if !ok {
		return defaultCircuitPolicy()
	}
	policy := circuitPolicy{
		enabled:          configured.Enabled,
		failureThreshold: configured.FailureThreshold,
		openFor:          configured.OpenFor,
	}
	if policy.failureThreshold <= 0 {
		policy.failureThreshold = defaultCircuitFailureThreshold
	}
	if policy.openFor <= 0 {
		policy.openFor = defaultCircuitOpenFor
	}
	return policy
}

func (g *Gateway) Reload(rt config.Runtime) (ReloadResult, error) {
	g.reloadMu.Lock()
	defer g.reloadMu.Unlock()

	old := g.currentRuntime()
	if reflect.DeepEqual(old.cfg.Config, rt.Config) {
		return ReloadResult{Changed: false}, nil
	}
	if err := validateHotReload(old.cfg, rt); err != nil {
		g.recordReloadFailure(err)
		return ReloadResult{}, err
	}

	next, err := buildRuntimeSnapshot(g, rt, &old)
	if err != nil {
		g.recordReloadFailure(err)
		return ReloadResult{}, err
	}

	g.runtimeMu.Lock()
	g.runtime = next
	g.runtimeMu.Unlock()
	g.logs.Resize(rt.Config.Logging.Recent)

	now := time.Now()
	g.logs.Add(LogEntry{
		Time: now, Level: "info", Client: "system",
		Message: fmt.Sprintf("config reloaded atomically: %d providers, %d enabled clients", len(next.providers), len(next.tokens)),
	})
	return ReloadResult{Changed: true, Reloaded: now, Providers: len(next.providers), Clients: len(next.tokens)}, nil
}

func (g *Gateway) RecordReloadFailure(err error) {
	if err != nil {
		g.recordReloadFailure(err)
	}
}

func (g *Gateway) recordReloadFailure(err error) {
	g.logs.Add(LogEntry{Level: "error", Client: "system", Message: "config reload rejected: " + err.Error()})
}

func validateHotReload(old, next config.Runtime) error {
	if old.Config.Server.Listen != next.Config.Server.Listen {
		return fmt.Errorf("server.listen change requires restart (%q -> %q)", old.Config.Server.Listen, next.Config.Server.Listen)
	}
	if old.ReadTimeout != next.ReadTimeout {
		return fmt.Errorf("server.read_timeout change requires restart")
	}
	if old.WriteTimeout != next.WriteTimeout {
		return fmt.Errorf("server.write_timeout change requires restart")
	}
	if old.IdleTimeout != next.IdleTimeout {
		return fmt.Errorf("server.idle_timeout change requires restart")
	}
	return nil
}
