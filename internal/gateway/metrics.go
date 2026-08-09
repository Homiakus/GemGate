package gateway

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	StartedAt      time.Time
	Requests       atomic.Uint64
	Requests2xx    atomic.Uint64
	Requests4xx    atomic.Uint64
	Requests5xx    atomic.Uint64
	BytesIn        atomic.Uint64
	BytesOut       atomic.Uint64
	InFlight       atomic.Int64
	UpstreamErrors atomic.Uint64
	AuthFailures   atomic.Uint64
	RateLimited    atomic.Uint64

	providersMu sync.RWMutex
	providers   map[string]*providerMetrics
}

type providerMetrics struct {
	mu                  sync.Mutex
	requests            uint64
	requests2xx         uint64
	requests4xx         uint64
	requests5xx         uint64
	transportErrors     uint64
	inFlight            int64
	duration            time.Duration
	lastDuration        time.Duration
	lastStatus          int
	lastRequest         time.Time
	consecutiveFailures uint64
}

type ProviderMetricsSnapshot struct {
	Name                string
	Requests            uint64
	Requests2xx         uint64
	Requests4xx         uint64
	Requests5xx         uint64
	TransportErrors     uint64
	InFlight            int64
	TotalDuration       time.Duration
	AverageDuration     time.Duration
	LastDuration        time.Duration
	LastStatus          int
	LastRequest         time.Time
	ConsecutiveFailures uint64
	Health              string
}

type MetricsSnapshot struct {
	StartedAt      time.Time
	Uptime         time.Duration
	Requests       uint64
	Requests2xx    uint64
	Requests4xx    uint64
	Requests5xx    uint64
	BytesIn        uint64
	BytesOut       uint64
	InFlight       int64
	UpstreamErrors uint64
	AuthFailures   uint64
	RateLimited    uint64
	Providers      []ProviderMetricsSnapshot
}

func NewMetrics() *Metrics {
	return &Metrics{
		StartedAt: time.Now(),
		providers: make(map[string]*providerMetrics),
	}
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		StartedAt:      m.StartedAt,
		Uptime:         time.Since(m.StartedAt),
		Requests:       m.Requests.Load(),
		Requests2xx:    m.Requests2xx.Load(),
		Requests4xx:    m.Requests4xx.Load(),
		Requests5xx:    m.Requests5xx.Load(),
		BytesIn:        m.BytesIn.Load(),
		BytesOut:       m.BytesOut.Load(),
		InFlight:       m.InFlight.Load(),
		UpstreamErrors: m.UpstreamErrors.Load(),
		AuthFailures:   m.AuthFailures.Load(),
		RateLimited:    m.RateLimited.Load(),
		Providers:      m.providerSnapshots(),
	}
}

func (m *Metrics) providerBegin(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	p := m.provider(name)
	p.mu.Lock()
	p.inFlight++
	p.mu.Unlock()
}

func (m *Metrics) providerFinish(name string, status int, duration time.Duration, transportError bool) {
	if strings.TrimSpace(name) == "" {
		return
	}
	p := m.provider(name)
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.inFlight > 0 {
		p.inFlight--
	}
	p.requests++
	p.duration += duration
	p.lastDuration = duration
	p.lastStatus = status
	p.lastRequest = time.Now()

	switch {
	case status >= 200 && status < 300:
		p.requests2xx++
	case status >= 400 && status < 500:
		p.requests4xx++
	case status >= 500:
		p.requests5xx++
	}
	if transportError {
		p.transportErrors++
	}

	if transportError || status >= 500 {
		p.consecutiveFailures++
	} else {
		p.consecutiveFailures = 0
	}
}

func (m *Metrics) provider(name string) *providerMetrics {
	m.providersMu.RLock()
	p := m.providers[name]
	m.providersMu.RUnlock()
	if p != nil {
		return p
	}

	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	if p = m.providers[name]; p == nil {
		p = &providerMetrics{}
		m.providers[name] = p
	}
	return p
}

func (m *Metrics) providerSnapshots() []ProviderMetricsSnapshot {
	m.providersMu.RLock()
	names := make([]string, 0, len(m.providers))
	entries := make(map[string]*providerMetrics, len(m.providers))
	for name, p := range m.providers {
		names = append(names, name)
		entries[name] = p
	}
	m.providersMu.RUnlock()
	sort.Strings(names)

	out := make([]ProviderMetricsSnapshot, 0, len(names))
	for _, name := range names {
		p := entries[name]
		p.mu.Lock()
		s := ProviderMetricsSnapshot{
			Name:                name,
			Requests:            p.requests,
			Requests2xx:         p.requests2xx,
			Requests4xx:         p.requests4xx,
			Requests5xx:         p.requests5xx,
			TransportErrors:     p.transportErrors,
			InFlight:            p.inFlight,
			TotalDuration:       p.duration,
			LastDuration:        p.lastDuration,
			LastStatus:          p.lastStatus,
			LastRequest:         p.lastRequest,
			ConsecutiveFailures: p.consecutiveFailures,
		}
		if p.requests > 0 {
			s.AverageDuration = p.duration / time.Duration(p.requests)
		}
		s.Health = providerHealth(s)
		p.mu.Unlock()
		out = append(out, s)
	}
	return out
}

func providerHealth(s ProviderMetricsSnapshot) string {
	if s.Requests == 0 {
		return "unknown"
	}
	switch {
	case s.ConsecutiveFailures >= 3:
		return "degraded"
	case s.ConsecutiveFailures > 0:
		return "warning"
	default:
		return "healthy"
	}
}

func (s MetricsSnapshot) Prometheus() string {
	var b strings.Builder
	fmt.Fprintf(&b, `# HELP gemgate_requests_total Total proxied requests.
# TYPE gemgate_requests_total counter
gemgate_requests_total %d
# HELP gemgate_requests_2xx_total Total upstream 2xx responses.
# TYPE gemgate_requests_2xx_total counter
gemgate_requests_2xx_total %d
# HELP gemgate_requests_4xx_total Total upstream/client 4xx responses.
# TYPE gemgate_requests_4xx_total counter
gemgate_requests_4xx_total %d
# HELP gemgate_requests_5xx_total Total upstream/proxy 5xx responses.
# TYPE gemgate_requests_5xx_total counter
gemgate_requests_5xx_total %d
# HELP gemgate_inflight Current in-flight requests.
# TYPE gemgate_inflight gauge
gemgate_inflight %d
# HELP gemgate_bytes_in_total Request bytes received.
# TYPE gemgate_bytes_in_total counter
gemgate_bytes_in_total %d
# HELP gemgate_bytes_out_total Response bytes sent.
# TYPE gemgate_bytes_out_total counter
gemgate_bytes_out_total %d
# HELP gemgate_auth_failures_total Authentication failures.
# TYPE gemgate_auth_failures_total counter
gemgate_auth_failures_total %d
# HELP gemgate_rate_limited_total Requests rejected by per-client rate limits.
# TYPE gemgate_rate_limited_total counter
gemgate_rate_limited_total %d
# HELP gemgate_upstream_errors_total Upstream transport/proxy errors.
# TYPE gemgate_upstream_errors_total counter
gemgate_upstream_errors_total %d
`, s.Requests, s.Requests2xx, s.Requests4xx, s.Requests5xx, s.InFlight, s.BytesIn, s.BytesOut, s.AuthFailures, s.RateLimited, s.UpstreamErrors)

	if len(s.Providers) == 0 {
		return b.String()
	}

	b.WriteString(`# HELP gemgate_provider_requests_total Completed provider requests by response class.
# TYPE gemgate_provider_requests_total counter
# HELP gemgate_provider_inflight Current in-flight requests by provider.
# TYPE gemgate_provider_inflight gauge
# HELP gemgate_provider_transport_errors_total Provider transport failures.
# TYPE gemgate_provider_transport_errors_total counter
# HELP gemgate_provider_request_duration_seconds Provider request duration including streamed response body.
# TYPE gemgate_provider_request_duration_seconds summary
# HELP gemgate_provider_consecutive_failures Consecutive provider transport/5xx failures.
# TYPE gemgate_provider_consecutive_failures gauge
`)
	for _, p := range s.Providers {
		label := prometheusLabel(p.Name)
		fmt.Fprintf(&b, "gemgate_provider_requests_total{provider=\"%s\",status_class=\"2xx\"} %d\n", label, p.Requests2xx)
		fmt.Fprintf(&b, "gemgate_provider_requests_total{provider=\"%s\",status_class=\"4xx\"} %d\n", label, p.Requests4xx)
		fmt.Fprintf(&b, "gemgate_provider_requests_total{provider=\"%s\",status_class=\"5xx\"} %d\n", label, p.Requests5xx)
		fmt.Fprintf(&b, "gemgate_provider_requests_total{provider=\"%s\",status_class=\"transport_error\"} %d\n", label, p.TransportErrors)
		fmt.Fprintf(&b, "gemgate_provider_inflight{provider=\"%s\"} %d\n", label, p.InFlight)
		fmt.Fprintf(&b, "gemgate_provider_transport_errors_total{provider=\"%s\"} %d\n", label, p.TransportErrors)
		fmt.Fprintf(&b, "gemgate_provider_request_duration_seconds_sum{provider=\"%s\"} %.6f\n", label, p.TotalDuration.Seconds())
		fmt.Fprintf(&b, "gemgate_provider_request_duration_seconds_count{provider=\"%s\"} %d\n", label, p.Requests)
		fmt.Fprintf(&b, "gemgate_provider_consecutive_failures{provider=\"%s\"} %d\n", label, p.ConsecutiveFailures)
	}
	return b.String()
}

func prometheusLabel(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
	return r.Replace(s)
}

func recordStatus(m *Metrics, status int) {
	m.Requests.Add(1)
	switch {
	case status >= 200 && status < 300:
		m.Requests2xx.Add(1)
	case status >= 400 && status < 500:
		m.Requests4xx.Add(1)
	case status >= 500:
		m.Requests5xx.Add(1)
	}
}
