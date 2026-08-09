package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreakerHalfOpenRecovery(t *testing.T) {
	breaker := newCircuitBreaker(2, 10*time.Second)
	now := time.Unix(100, 0)

	permit, ok, _ := breaker.allow(now)
	if !ok {
		t.Fatal("initial request should be allowed")
	}
	breaker.finish(permit, true, now)

	permit, ok, _ = breaker.allow(now.Add(time.Second))
	if !ok {
		t.Fatal("second request should be allowed before threshold")
	}
	breaker.finish(permit, true, now.Add(time.Second))

	if _, ok, retry := breaker.allow(now.Add(2 * time.Second)); ok || retry <= 0 {
		t.Fatalf("open circuit should reject with retry duration, ok=%t retry=%s", ok, retry)
	}

	probe, ok, _ := breaker.allow(now.Add(12 * time.Second))
	if !ok || !probe.probe {
		t.Fatal("first request after cooldown should be a half-open probe")
	}
	if _, ok, _ := breaker.allow(now.Add(12 * time.Second)); ok {
		t.Fatal("second half-open request should be rejected while probe is in flight")
	}
	breaker.finish(probe, false, now.Add(12*time.Second))

	if snapshot := breaker.snapshot(now.Add(12 * time.Second)); snapshot.State != string(circuitClosed) || snapshot.Failures != 0 {
		t.Fatalf("breaker did not recover: %#v", snapshot)
	}
}

func TestProviderTransportStopsCallingOpenCircuit(t *testing.T) {
	var calls atomic.Int64
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("upstream failure")),
			Request:    req,
		}, nil
	})
	metrics := NewMetrics()
	transport := newProviderMetricsTransport("test-provider", base, metrics)

	for i := 0; i < defaultCircuitFailureThreshold; i++ {
		req, _ := http.NewRequest(http.MethodPost, "https://provider.test/v1/generate", nil)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	req, _ := http.NewRequest(http.MethodPost, "https://provider.test/v1/generate", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if resp.Header.Get("X-GemGate-Circuit") != "open" || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("missing circuit headers: %#v", resp.Header)
	}
	if got := calls.Load(); got != defaultCircuitFailureThreshold {
		t.Fatalf("upstream calls = %d, want %d", got, defaultCircuitFailureThreshold)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
