package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gemgate/internal/config"
	"gemgate/internal/provider"
)

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
	"content-length":      {},
}

var upstreamCredentialHeaders = map[string]struct{}{
	"authorization":       {},
	"x-goog-api-key":      {},
	"x-goog-user-project": {},
	"x-api-key":           {},
	"api-key":             {},
	"anthropic-version":   {},
}

var errBodyTooLarge = errors.New("request body too large")

type Gateway struct {
	cfg             config.Runtime
	providers       map[string]*providerRuntime
	defaultProvider *providerRuntime
	metrics         *Metrics
	logs            *LogRing
	tokens          map[string]clientAuth // token -> client settings
	limits          map[string]*rateWindow
	server          *http.Server
	started         time.Time
}

type providerRuntime struct {
	name    string
	spec    provider.Spec
	baseURL *url.URL
	apiKey  string
	headers map[string]string
	client  *http.Client
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
	Name             string
	Type             string
	BaseURL          string
	APIKey           string
	Timeout          string
	OpenAICompatible bool
}

type ConfigSnapshot struct {
	Listen           string
	PublicHealth     bool
	RequestBodyLimit string
	UpstreamBaseURL  string // compatibility alias for default provider
	UpstreamAPIKey   string // compatibility alias for default provider
	DefaultProvider  string
	Providers        []ProviderSnapshot
	LogRecent        int
	Clients          []ClientSnapshot
	CORSEnabled      bool
	CORSOrigins      []string
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

	metrics := NewMetrics()
	providers := make(map[string]*providerRuntime, len(rt.Config.Providers))
	for _, p := range rt.Config.Providers {
		spec, ok := provider.Lookup(p.Type)
		if !ok {
			return nil, fmt.Errorf("provider %q has unsupported type %q", p.Name, p.Type)
		}
		u, err := url.Parse(strings.TrimRight(p.BaseURL, "/"))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("provider %q has invalid base_url", p.Name)
		}
		headers := make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			headers[k] = v
		}
		metrics.provider(p.Name) // expose configured providers even before first traffic
		providers[p.Name] = &providerRuntime{
			name: p.Name, spec: spec, baseURL: u, apiKey: p.APIKey, headers: headers,
			client: &http.Client{
				Timeout:   rt.ProviderTimeouts[p.Name],
				Transport: newProviderMetricsTransport(p.Name, transport, metrics),
			},
		}
	}
	defaultProvider := providers[rt.Config.DefaultProvider]
	if defaultProvider == nil {
		return nil, fmt.Errorf("default provider %q is not configured", rt.Config.DefaultProvider)
	}

	tokens := make(map[string]clientAuth)
	limits := make(map[string]*rateWindow)
	for _, c := range rt.Config.Clients {
		if c.Enabled {
			tokens[c.Token] = clientAuth{Name: c.Name, RateLimitRPM: c.RateLimitRPM}
			if c.RateLimitRPM > 0 {
				limits[c.Token] = &rateWindow{}
			}
		}
	}

	gw := &Gateway{
		cfg:             rt,
		providers:       providers,
		defaultProvider: defaultProvider,
		metrics:         metrics,
		logs:            NewLogRing(rt.Config.Logging.Recent),
		tokens:          tokens,
		limits:          limits,
		started:         time.Now(),
	}
	gw.server = &http.Server{
		Addr:              rt.Config.Server.Listen,
		Handler:           newCORSHandler(gw, rt.Config.Server.CORS, rt.CORSMaxAge),
		ReadTimeout:       rt.ReadTimeout,
		ReadHeaderTimeout: minDuration(10*time.Second, rt.ReadTimeout, 10*time.Second),
		WriteTimeout:      rt.WriteTimeout,
		IdleTimeout:       rt.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}
	return gw, nil
}

func (g *Gateway) ListenAndServe() error {
	g.logs.Add(LogEntry{Level: "info", Message: "listening on " + g.cfg.Config.Server.Listen, Client: "system"})
	if err := g.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (g *Gateway) Shutdown(ctx context.Context) error {
	g.logs.Add(LogEntry{Level: "info", Message: "server shutdown requested", Client: "system"})
	return g.server.Shutdown(ctx)
}

func (g *Gateway) Addr() string             { return g.cfg.Config.Server.Listen }
func (g *Gateway) Metrics() MetricsSnapshot { return g.metrics.Snapshot() }
func (g *Gateway) Logs() []LogEntry         { return g.logs.Snapshot() }

func (g *Gateway) ConfigSnapshot() ConfigSnapshot {
	clients := make([]ClientSnapshot, 0, len(g.cfg.Config.Clients))
	for _, c := range g.cfg.Config.Clients {
		clients = append(clients, ClientSnapshot{Name: c.Name, Enabled: c.Enabled, Token: redact(c.Token), RateLimitRPM: c.RateLimitRPM})
	}
	providers := make([]ProviderSnapshot, 0, len(g.cfg.Config.Providers))
	for _, p := range g.cfg.Config.Providers {
		spec, _ := provider.Lookup(p.Type)
		providers = append(providers, ProviderSnapshot{
			Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, APIKey: redact(p.APIKey), Timeout: p.Timeout,
			OpenAICompatible: spec.OpenAICompatible,
		})
	}
	return ConfigSnapshot{
		Listen: g.cfg.Config.Server.Listen, PublicHealth: g.cfg.Config.Server.PublicHealth,
		RequestBodyLimit: g.cfg.Config.Server.RequestBodyLimit,
		UpstreamBaseURL:  g.defaultProvider.baseURL.String(), UpstreamAPIKey: redact(g.defaultProvider.apiKey),
		DefaultProvider: g.cfg.Config.DefaultProvider, Providers: providers,
		LogRecent: g.cfg.Config.Logging.Recent, Clients: clients,
		CORSEnabled: g.cfg.Config.Server.CORS.IsEnabled(),
		CORSOrigins: append([]string(nil), g.cfg.Config.Server.CORS.AllowedOrigins...),
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := requestID(r)
	w.Header().Set("X-Request-ID", reqID)

	if r.URL.Path == "/_healthz" && g.cfg.Config.Server.PublicHealth {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"service":   "gemgate",
			"uptime":    time.Since(g.started).String(),
			"providers": providerHealthSnapshot(g.Metrics().Providers),
		})
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	auth, token, ok := g.authenticate(r)
	if !ok {
		g.metrics.AuthFailures.Add(1)
		recordStatus(g.metrics, http.StatusUnauthorized)
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: "anonymous", Method: r.Method, Path: r.URL.Path, Status: http.StatusUnauthorized, Duration: time.Since(start), RequestID: reqID, Message: "auth failed"})
		http.Error(w, "invalid proxy token", http.StatusUnauthorized)
		return
	}

	if r.URL.Path == "/_metrics" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(g.Metrics().Prometheus()))
		return
	}
	if r.URL.Path == "/_config" {
		writeJSON(w, http.StatusOK, g.safeConfig())
		return
	}

	if !g.allowClient(token, auth.RateLimitRPM) {
		g.metrics.RateLimited.Add(1)
		recordStatus(g.metrics, http.StatusTooManyRequests)
		reset := g.rateLimitReset(token)
		if reset > 0 {
			retryAfter := max(1, int(reset.Round(time.Second).Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: auth.Name, Method: r.Method, Path: r.URL.Path, Status: http.StatusTooManyRequests, Duration: time.Since(start), RequestID: reqID, Message: "rate limit exceeded"})
		http.Error(w, "client rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	g.metrics.InFlight.Add(1)
	defer g.metrics.InFlight.Add(-1)

	result, err := g.proxy(w, r, auth.Name, reqID)
	if err != nil {
		g.metrics.UpstreamErrors.Add(1)
		if result.status == 0 {
			result.status = http.StatusBadGateway
		}
		if !result.responseStarted {
			http.Error(w, publicProxyError(result.status), result.status)
		}
	}
	recordStatus(g.metrics, result.status)
	message := ""
	if err != nil {
		message = err.Error()
	}
	g.logs.Add(LogEntry{
		Time: start, Level: levelForStatus(result.status), Client: auth.Name, Provider: result.provider,
		Method: r.Method, Path: r.URL.RequestURI(), Status: result.status, Bytes: result.bytesOut,
		Duration: time.Since(start), RequestID: reqID, Message: message,
	})
}

func (g *Gateway) proxy(w http.ResponseWriter, r *http.Request, clientName, reqID string) (proxyResult, error) {
	p, targetPath, status, err := g.resolveProvider(r.URL.Path)
	if err != nil {
		return proxyResult{status: status}, err
	}
	result := proxyResult{provider: p.name}

	body, contentLength, err := g.prepareBody(r)
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

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		result.status = http.StatusInternalServerError
		return result, fmt.Errorf("build upstream request: %w", err)
	}
	req.ContentLength = contentLength
	copyRequestHeaders(req.Header, r.Header)
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
	if err != nil && !errors.Is(err, context.Canceled) {
		return result, fmt.Errorf("stream provider %q response: %w", p.name, err)
	}
	return result, nil
}

func (g *Gateway) resolveProvider(path string) (*providerRuntime, string, int, error) {
	const prefix = "/providers/"
	if !strings.HasPrefix(path, prefix) {
		return g.defaultProvider, path, 0, nil
	}
	rest := strings.TrimPrefix(path, prefix)
	idx := strings.IndexByte(rest, '/')
	if idx <= 0 {
		return nil, "", http.StatusBadRequest, errors.New("provider route must be /providers/{name}/{path}")
	}
	name := rest[:idx]
	p := g.providers[name]
	if p == nil {
		return nil, "", http.StatusNotFound, fmt.Errorf("unknown provider %q", name)
	}
	return p, "/" + rest[idx+1:], 0, nil
}

func (g *Gateway) prepareBody(r *http.Request) (io.Reader, int64, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, 0, nil
	}
	limit := g.cfg.RequestBodyLimit
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

func (g *Gateway) authenticate(r *http.Request) (clientAuth, string, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return clientAuth{}, "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return clientAuth{}, "", false
	}
	info, ok := g.tokens[token]
	return info, token, ok
}

func (g *Gateway) allowClient(token string, limit int) bool {
	window := g.limits[token]
	if window == nil {
		return true
	}
	ok, _ := window.allow(limit, time.Now())
	return ok
}

func (g *Gateway) rateLimitReset(token string) time.Duration {
	window := g.limits[token]
	if window == nil {
		return 0
	}
	window.mu.Lock()
	defer window.mu.Unlock()
	if window.start.IsZero() {
		return 0
	}
	reset := time.Minute - time.Since(window.start)
	if reset < 0 {
		return 0
	}
	return reset
}

func (g *Gateway) safeConfig() map[string]any {
	clients := make([]map[string]any, 0, len(g.cfg.Config.Clients))
	for _, c := range g.cfg.Config.Clients {
		clients = append(clients, map[string]any{"name": c.Name, "enabled": c.Enabled, "token": redact(c.Token), "rate_limit_rpm": c.RateLimitRPM})
	}
	providers := make([]map[string]any, 0, len(g.cfg.Config.Providers))
	for _, p := range g.cfg.Config.Providers {
		providers = append(providers, map[string]any{
			"name": p.Name, "type": p.Type, "base_url": p.BaseURL, "api_key": redact(p.APIKey), "timeout": p.Timeout,
		})
	}
	return map[string]any{
		"server": map[string]any{
			"listen":             g.cfg.Config.Server.Listen,
			"public_health":      g.cfg.Config.Server.PublicHealth,
			"request_body_limit": g.cfg.Config.Server.RequestBodyLimit,
			"cors": map[string]any{
				"enabled":           g.cfg.Config.Server.CORS.IsEnabled(),
				"allowed_origins":   g.cfg.Config.Server.CORS.AllowedOrigins,
				"allowed_methods":   g.cfg.Config.Server.CORS.AllowedMethods,
				"allowed_headers":   g.cfg.Config.Server.CORS.AllowedHeaders,
				"allow_credentials": g.cfg.Config.Server.CORS.AllowCredentials,
				"max_age":           g.cfg.Config.Server.CORS.MaxAge,
			},
		},
		"default_provider": g.cfg.Config.DefaultProvider,
		"providers":        providers,
		"clients":          clients,
	}
}

func providerHealthSnapshot(providers []ProviderMetricsSnapshot) map[string]string {
	out := make(map[string]string, len(providers))
	for _, p := range providers {
		out[p.Name] = p.Health
	}
	return out
}

func copyRequestHeaders(dst, src http.Header) {
	connectionHeaders := connectionHeaderNames(src)
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		if _, skip := connectionHeaders[lk]; skip {
			continue
		}
		if _, secret := upstreamCredentialHeaders[lk]; secret {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	connectionHeaders := connectionHeaderNames(src)
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		if _, skip := connectionHeaders[lk]; skip {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func connectionHeaderNames(h http.Header) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				out[name] = struct{}{}
			}
		}
	}
	return out
}

func copyAndFlush(w http.ResponseWriter, r io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	flusher, _ := w.(http.Flusher)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			wn, writeErr := w.Write(buf[:n])
			written += int64(wn)
			if flusher != nil {
				flusher.Flush()
			}
			if writeErr != nil {
				return written, writeErr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func requestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); validRequestID(id) {
		return id
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func publicProxyError(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid proxy request"
	case http.StatusNotFound:
		return "provider not found"
	case http.StatusRequestEntityTooLarge:
		return "request body too large"
	case http.StatusInternalServerError:
		return "gateway configuration error"
	default:
		return "upstream provider request failed"
	}
}

func levelForStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warn"
	default:
		return "info"
	}
}

func redact(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func minDuration(values ...time.Duration) time.Duration {
	var out time.Duration
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if out == 0 || v < out {
			out = v
		}
	}
	return out
}
