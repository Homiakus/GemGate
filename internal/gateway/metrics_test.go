package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestProviderMetricsTransportRecordsCompletedResponse(t *testing.T) {
	metrics := NewMetrics()
	transport := newProviderMetricsTransport("openai", roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		}, nil
	}), metrics)

	req, err := http.NewRequest(http.MethodGet, "https://example.test/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	before := metrics.Snapshot().Providers[0]
	if before.InFlight != 1 {
		t.Fatalf("in-flight before body close = %d, want 1", before.InFlight)
	}

	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	s := metrics.Snapshot()
	if len(s.Providers) != 1 {
		t.Fatalf("providers = %#v", s.Providers)
	}
	p := s.Providers[0]
	if p.Name != "openai" || p.Requests != 1 || p.Requests2xx != 1 || p.InFlight != 0 {
		t.Fatalf("provider metrics = %#v", p)
	}
	if p.Health != "healthy" {
		t.Fatalf("health = %q", p.Health)
	}
	if p.TotalDuration <= 0 {
		t.Fatal("expected positive provider duration")
	}
	if !strings.Contains(s.Prometheus(), `gemgate_provider_requests_total{provider="openai",status_class="2xx"} 1`) {
		t.Fatal("Prometheus output does not contain provider request metric")
	}
}

func TestProviderHealthBecomesDegradedAfterConsecutiveFailures(t *testing.T) {
	metrics := NewMetrics()
	for range 3 {
		metrics.providerBegin("anthropic")
		metrics.providerFinish("anthropic", http.StatusBadGateway, time.Millisecond, false)
	}
	p := metrics.Snapshot().Providers[0]
	if p.Health != "degraded" {
		t.Fatalf("health = %q, want degraded", p.Health)
	}
	if p.ConsecutiveFailures != 3 {
		t.Fatalf("consecutive failures = %d", p.ConsecutiveFailures)
	}
}
