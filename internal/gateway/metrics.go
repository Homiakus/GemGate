package gateway

import (
	"fmt"
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
}

func NewMetrics() *Metrics {
	return &Metrics{StartedAt: time.Now()}
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
	}
}

func (s MetricsSnapshot) Prometheus() string {
	return fmt.Sprintf(`# HELP gemgate_requests_total Total proxied requests.
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
# HELP gemgate_upstream_errors_total Upstream transport errors.
# TYPE gemgate_upstream_errors_total counter
gemgate_upstream_errors_total %d
`, s.Requests, s.Requests2xx, s.Requests4xx, s.Requests5xx, s.InFlight, s.BytesIn, s.BytesOut, s.AuthFailures, s.RateLimited, s.UpstreamErrors)
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
