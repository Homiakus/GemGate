package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"gemgate/internal/config"
	"gemgate/internal/provider"
)

type Gateway struct {
	runtimeMu sync.RWMutex
	reloadMu  sync.Mutex
	runtime   runtimeSnapshot

	transport  *http.Transport
	rateLimits *rateLimitManager
	metrics    *Metrics
	logs       *LogRing
	server     *http.Server
	started    time.Time
}

type providerRuntime struct {
	name    string
	spec    provider.Spec
	baseURL *url.URL
	apiKey  string
	headers map[string]string
	client  *http.Client
	breaker *circuitBreaker
}

type clientAuth struct {
	Name         string
	RateLimitRPM int
}

type ClientSnapshot struct {
	Name         string
	Enabled      bool
	Token        string
	RateLimitRPM int
}

type ProviderSnapshot struct {
	Name                    string
	Type                    string
	BaseURL                 string
	APIKey                  string
	Timeout                 string
	OpenAICompatible        bool
	CircuitEnabled          bool
	CircuitFailureThreshold int
	CircuitOpenFor          string
}

type ConfigSnapshot struct {
	Listen                  string
	PublicHealth            bool
	RequestBodyLimit        string
	TrustedProxies          []string
	DedicatedOperationsAuth bool
	RateLimitBackend        string
	RateLimitFailOpen       bool
	UpstreamBaseURL         string // compatibility alias for default provider
	UpstreamAPIKey          string // compatibility alias for default provider
	DefaultProvider         string
	Providers               []ProviderSnapshot
	LogRecent               int
	Clients                 []ClientSnapshot
	CORSEnabled             bool
	CORSOrigins             []string
}

type proxyResult struct {
	status          int
	bytesOut        int64
	responseStarted bool
	provider        string
}

func New(rt config.Runtime) (*Gateway, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.MaxIdleConns = 200
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second
	transport.ResponseHeaderTimeout = 0 // model generation may take a long time before first byte

	rateLimits, err := newRateLimitManager(rt)
	if err != nil {
		return nil, err
	}
	gw := &Gateway{
		transport:  transport,
		rateLimits: rateLimits,
		metrics:    NewMetrics(),
		logs:       NewLogRing(rt.Config.Logging.Recent),
		started:    time.Now(),
	}
	state, err := buildRuntimeSnapshot(gw, rt, nil)
	if err != nil {
		_ = gw.rateLimits.Close()
		return nil, err
	}
	gw.runtime = state
	gw.rateLimits.RetainTokens(state.tokens)
	gw.server = &http.Server{
		Addr:              rt.Config.Server.Listen,
		Handler:           gw,
		ReadTimeout:       rt.ReadTimeout,
		ReadHeaderTimeout: minDuration(10*time.Second, rt.ReadTimeout, 10*time.Second),
		WriteTimeout:      rt.WriteTimeout,
		IdleTimeout:       rt.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}
	return gw, nil
}

func (g *Gateway) currentRuntime() runtimeSnapshot {
	g.runtimeMu.RLock()
	state := g.runtime
	g.runtimeMu.RUnlock()
	return state
}

func (g *Gateway) ListenAndServe() error {
	g.logs.Add(LogEntry{Level: "info", Message: "listening on " + g.server.Addr, Client: "system"})
	if err := g.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (g *Gateway) Shutdown(ctx context.Context) error {
	g.logs.Add(LogEntry{Level: "info", Message: "server shutdown requested", Client: "system"})
	serverErr := g.server.Shutdown(ctx)
	limitErr := g.rateLimits.Close()
	if serverErr != nil {
		return serverErr
	}
	return limitErr
}

func (g *Gateway) Addr() string { return g.server.Addr }

func (g *Gateway) Metrics() MetricsSnapshot {
	snapshot := g.metrics.Snapshot()
	state := g.currentRuntime()
	snapshot.Circuits = circuitSnapshots(state, time.Now())
	return snapshot
}

func (g *Gateway) Logs() []LogEntry { return g.logs.Snapshot() }

func (g *Gateway) ConfigSnapshot() ConfigSnapshot {
	state := g.currentRuntime()
	clients := make([]ClientSnapshot, 0, len(state.cfg.Config.Clients))
	for _, c := range state.cfg.Config.Clients {
		clients = append(clients, ClientSnapshot{Name: c.Name, Enabled: c.Enabled, Token: redact(c.Token), RateLimitRPM: c.RateLimitRPM})
	}
	providers := make([]ProviderSnapshot, 0, len(state.cfg.Config.Providers))
	for _, p := range state.cfg.Config.Providers {
		spec, _ := provider.Lookup(p.Type)
		policy := circuitPolicyFor(state.cfg, p.Name)
		providers = append(providers, ProviderSnapshot{
			Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, APIKey: redact(p.APIKey), Timeout: p.Timeout,
			OpenAICompatible: spec.OpenAICompatible,
			CircuitEnabled:   policy.enabled, CircuitFailureThreshold: policy.failureThreshold, CircuitOpenFor: policy.openFor.String(),
		})
	}
	return ConfigSnapshot{
		Listen: state.cfg.Config.Server.Listen, PublicHealth: state.cfg.Config.Server.PublicHealth,
		RequestBodyLimit: state.cfg.Config.Server.RequestBodyLimit,
		TrustedProxies:   append([]string(nil), state.cfg.Config.Server.TrustedProxies...),
		DedicatedOperationsAuth: state.operationsToken != "",
		RateLimitBackend: g.rateLimits.Name(), RateLimitFailOpen: g.rateLimits.FailOpen(),
		UpstreamBaseURL: state.defaultProvider.baseURL.String(), UpstreamAPIKey: redact(state.defaultProvider.apiKey),
		DefaultProvider: state.cfg.Config.DefaultProvider, Providers: providers,
		LogRecent: state.cfg.Config.Logging.Recent, Clients: clients,
		CORSEnabled: state.cfg.Config.Server.CORS.IsEnabled(),
		CORSOrigins: append([]string(nil), state.cfg.Config.Server.CORS.AllowedOrigins...),
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	state := g.currentRuntime()
	if state.cors != nil && state.cors.handle(w, r) {
		return
	}
	g.serveHTTP(state, w, r)
}

func (g *Gateway) serveHTTP(state runtimeSnapshot, w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := requestID(r)
	clientIP := resolveClientIP(state.cfg, r)
	w.Header().Set("X-Request-ID", reqID)

	if r.URL.Path == "/_healthz" && state.cfg.Config.Server.PublicHealth {
		g.writeHealth(state, w)
		return
	}
	if r.URL.Path == "/_readyz" && state.cfg.Config.Server.PublicHealth {
		g.writeReadiness(state, w)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if isOperationalPath(r.URL.Path) {
		if !operationsAuthorized(state, r) {
			g.metrics.AuthFailures.Add(1)
			recordStatus(g.metrics, http.StatusUnauthorized)
			g.logs.Add(LogEntry{Time: start, Level: "warn", Client: "operations", ClientIP: clientIP, Method: r.Method, Path: r.URL.Path, Status: http.StatusUnauthorized, Duration: time.Since(start), RequestID: reqID, Message: "operations auth failed"})
			w.Header().Set("WWW-Authenticate", `Bearer realm="gemgate-operations"`)
			http.Error(w, "invalid operations token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/_healthz":
			g.writeHealth(state, w)
		case "/_readyz":
			g.writeReadiness(state, w)
		case "/_metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(g.Metrics().Prometheus()))
		case "/_config":
			writeJSON(w, http.StatusOK, safeConfig(state))
		}
		return
	}

	auth, token, ok := authenticate(state, r)
	if !ok {
		g.metrics.AuthFailures.Add(1)
		recordStatus(g.metrics, http.StatusUnauthorized)
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: "anonymous", ClientIP: clientIP, Method: r.Method, Path: r.URL.Path, Status: http.StatusUnauthorized, Duration: time.Since(start), RequestID: reqID, Message: "auth failed"})
		http.Error(w, "invalid proxy token", http.StatusUnauthorized)
		return
	}

	decision, limitErr := g.rateLimits.Allow(r.Context(), token, auth.RateLimitRPM, time.Now())
	if limitErr != nil {
		g.metrics.RateLimitBackendErrors.Add(1)
		if !decision.Allowed {
			recordStatus(g.metrics, http.StatusServiceUnavailable)
			g.logs.Add(LogEntry{Time: start, Level: "error", Client: auth.Name, ClientIP: clientIP, Method: r.Method, Path: r.URL.Path, Status: http.StatusServiceUnavailable, Duration: time.Since(start), RequestID: reqID, Message: "rate limit backend unavailable: " + limitErr.Error()})
			http.Error(w, "rate limit backend unavailable", http.StatusServiceUnavailable)
			return
		}
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: auth.Name, ClientIP: clientIP, Method: r.Method, Path: r.URL.Path, Duration: time.Since(start), RequestID: reqID, Message: "rate limit backend unavailable; fail-open allowed request: " + limitErr.Error()})
	}
	if !decision.Allowed {
		g.metrics.RateLimited.Add(1)
		recordStatus(g.metrics, http.StatusTooManyRequests)
		if decision.RetryAfter > 0 {
			retryAfter := max(1, int(decision.RetryAfter.Round(time.Second).Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: auth.Name, ClientIP: clientIP, Method: r.Method, Path: r.URL.Path, Status: http.StatusTooManyRequests, Duration: time.Since(start), RequestID: reqID, Message: "rate limit exceeded"})
		http.Error(w, "client rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	g.metrics.InFlight.Add(1)
	defer g.metrics.InFlight.Add(-1)

	result, err := g.proxy(state, w, r, auth.Name, reqID)
	clientAborted := err != nil && r.Context().Err() != nil
	if err != nil && !clientAborted {
		g.metrics.UpstreamErrors.Add(1)
		if result.status == 0 {
			result.status = http.StatusBadGateway
		}
		if !result.responseStarted {
			http.Error(w, publicProxyError(result.status), result.status)
		}
	}
	if !clientAborted {
		recordStatus(g.metrics, result.status)
	}

	message := ""
	level := levelForStatus(result.status)
	if err != nil {
		message = err.Error()
	}
	if clientAborted {
		result.status = 0
		level = "info"
		message = "client canceled request"
	}
	g.logs.Add(LogEntry{
		Time: start, Level: level, Client: auth.Name, ClientIP: clientIP, Provider: result.provider,
		Method: r.Method, Path: r.URL.RequestURI(), Status: result.status, Bytes: result.bytesOut,
		Duration: time.Since(start), RequestID: reqID, Message: message,
	})
}
