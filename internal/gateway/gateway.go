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

type Gateway struct {
	cfg      config.Runtime
	upstream *url.URL
	client   *http.Client
	metrics  *Metrics
	logs     *LogRing
	tokens   map[string]clientAuth // token -> client settings
	limits   map[string]*rateWindow
	server   *http.Server
	started  time.Time
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

type ConfigSnapshot struct {
	Listen           string
	PublicHealth     bool
	RequestBodyLimit string
	UpstreamBaseURL  string
	UpstreamAPIKey   string
	LogRecent        int
	Clients          []ClientSnapshot
}

func New(rt config.Runtime) (*Gateway, error) {
	u, err := url.Parse(strings.TrimRight(rt.Config.Upstream.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse upstream base_url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, errors.New("upstream.base_url must be absolute, for example https://generativelanguage.googleapis.com")
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

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.MaxIdleConns = 200
	transport.MaxIdleConnsPerHost = 200
	transport.IdleConnTimeout = 90 * time.Second

	gw := &Gateway{
		cfg:      rt,
		upstream: u,
		client: &http.Client{
			Timeout:   rt.UpstreamTimeout,
			Transport: transport,
		},
		metrics: NewMetrics(),
		logs:    NewLogRing(rt.Config.Logging.Recent),
		tokens:  tokens,
		limits:  limits,
		started: time.Now(),
	}
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

func (g *Gateway) Addr() string {
	return g.cfg.Config.Server.Listen
}

func (g *Gateway) Metrics() MetricsSnapshot {
	return g.metrics.Snapshot()
}

func (g *Gateway) Logs() []LogEntry {
	return g.logs.Snapshot()
}

func (g *Gateway) ConfigSnapshot() ConfigSnapshot {
	clients := make([]ClientSnapshot, 0, len(g.cfg.Config.Clients))
	for _, c := range g.cfg.Config.Clients {
		clients = append(clients, ClientSnapshot{
			Name:         c.Name,
			Enabled:      c.Enabled,
			Token:        redact(c.Token),
			RateLimitRPM: c.RateLimitRPM,
		})
	}
	return ConfigSnapshot{
		Listen:           g.cfg.Config.Server.Listen,
		PublicHealth:     g.cfg.Config.Server.PublicHealth,
		RequestBodyLimit: g.cfg.Config.Server.RequestBodyLimit,
		UpstreamBaseURL:  g.cfg.Config.Upstream.BaseURL,
		UpstreamAPIKey:   redact(g.cfg.Config.Upstream.APIKey),
		LogRecent:        g.cfg.Config.Logging.Recent,
		Clients:          clients,
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := requestID(r)
	w.Header().Set("X-Request-ID", requestID)

	if r.URL.Path == "/_healthz" && g.cfg.Config.Server.PublicHealth {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "gemgate", "uptime": time.Since(g.started).String()})
		return
	}

	if r.Method == http.MethodOptions {
		g.writeCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	auth, token, ok := g.authenticate(r)
	if !ok {
		g.metrics.AuthFailures.Add(1)
		recordStatus(g.metrics, http.StatusUnauthorized)
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: "anonymous", Method: r.Method, Path: r.URL.Path, Status: http.StatusUnauthorized, Duration: time.Since(start), RequestID: requestID, Message: "auth failed"})
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
			retryAfter := int(reset.Round(time.Second).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		g.logs.Add(LogEntry{Time: start, Level: "warn", Client: auth.Name, Method: r.Method, Path: r.URL.Path, Status: http.StatusTooManyRequests, Duration: time.Since(start), RequestID: requestID, Message: "rate limit exceeded"})
		http.Error(w, "client rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	g.metrics.InFlight.Add(1)
	defer g.metrics.InFlight.Add(-1)

	status, bytesOut, err := g.proxy(w, r, auth.Name, requestID)
	if err != nil {
		g.metrics.UpstreamErrors.Add(1)
		if status == 0 {
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
	}
	recordStatus(g.metrics, status)
	g.logs.Add(LogEntry{
		Time:      start,
		Level:     levelForStatus(status),
		Client:    auth.Name,
		Method:    r.Method,
		Path:      r.URL.RequestURI(),
		Status:    status,
		Bytes:     bytesOut,
		Duration:  time.Since(start),
		RequestID: requestID,
	})
}

func (g *Gateway) proxy(w http.ResponseWriter, r *http.Request, clientName, reqID string) (int, int64, error) {
	body, contentLength, err := g.prepareBody(r)
	if err != nil {
		return http.StatusRequestEntityTooLarge, 0, err
	}
	if contentLength > 0 {
		g.metrics.BytesIn.Add(uint64(contentLength))
	}

	upstreamURL := *g.upstream
	upstreamURL.Path = singleJoiningSlash(g.upstream.Path, strings.TrimPrefix(r.URL.Path, "/"))
	upstreamURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		return http.StatusInternalServerError, 0, err
	}
	req.ContentLength = contentLength
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("X-Request-ID", reqID)
	req.Header.Set("X-Gemgate-Client", clientName)
	req.Header.Set("Via", "gemgate")

	if isOpenAICompatPath(r.URL.Path) {
		req.Header.Set("Authorization", "Bearer "+g.cfg.Config.Upstream.APIKey)
		req.Header.Del("X-Goog-Api-Key")
	} else {
		req.Header.Set("X-Goog-Api-Key", g.cfg.Config.Upstream.APIKey)
		req.Header.Del("Authorization")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return http.StatusBadGateway, 0, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	g.writeCORS(w)
	w.WriteHeader(resp.StatusCode)

	bytesOut, copyErr := copyAndFlush(w, resp.Body)
	if bytesOut > 0 {
		g.metrics.BytesOut.Add(uint64(bytesOut))
	}
	if copyErr != nil && !errors.Is(copyErr, context.Canceled) {
		return resp.StatusCode, bytesOut, copyErr
	}
	return resp.StatusCode, bytesOut, nil
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
		return nil, 0, fmt.Errorf("request body is larger than configured limit %d bytes", limit)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(b)) > limit {
		return nil, 0, fmt.Errorf("request body is larger than configured limit %d bytes", limit)
	}
	return bytes.NewReader(b), int64(len(b)), nil
}

func (g *Gateway) authenticate(r *http.Request) (clientAuth, string, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return clientAuth{}, "", false
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return clientAuth{}, "", false
	}
	token := strings.TrimSpace(parts[1])
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

func (g *Gateway) writeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
}

func (g *Gateway) safeConfig() map[string]any {
	clients := make([]map[string]any, 0, len(g.cfg.Config.Clients))
	for _, c := range g.cfg.Config.Clients {
		clients = append(clients, map[string]any{"name": c.Name, "enabled": c.Enabled, "token": redact(c.Token), "rate_limit_rpm": c.RateLimitRPM})
	}
	return map[string]any{
		"server": map[string]any{
			"listen":             g.cfg.Config.Server.Listen,
			"public_health":      g.cfg.Config.Server.PublicHealth,
			"request_body_limit": g.cfg.Config.Server.RequestBodyLimit,
		},
		"upstream": map[string]any{
			"base_url": g.cfg.Config.Upstream.BaseURL,
			"api_key":  redact(g.cfg.Config.Upstream.APIKey),
		},
		"clients": clients,
	}
}

func copyRequestHeaders(dst, src http.Header) {
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		if lk == "authorization" || lk == "x-goog-api-key" || lk == "x-goog-user-project" {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		lk := strings.ToLower(k)
		if _, skip := hopByHopHeaders[lk]; skip {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
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
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" {
		return id
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

func isOpenAICompatPath(path string) bool {
	p := strings.TrimPrefix(path, "/")
	return strings.HasPrefix(p, "v1beta/openai/") || strings.HasPrefix(p, "v1/openai/")
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
