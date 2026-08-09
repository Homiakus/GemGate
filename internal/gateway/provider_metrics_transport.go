package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type providerMetricsTransport struct {
	provider string
	base     http.RoundTripper
	metrics  *Metrics
	breaker  *circuitBreaker
}

func newProviderMetricsTransport(provider string, base http.RoundTripper, metrics *Metrics, breaker *circuitBreaker) http.RoundTripper {
	if breaker == nil {
		breaker = newCircuitBreakerWithPolicy(defaultCircuitPolicy())
	}
	return &providerMetricsTransport{provider: provider, base: base, metrics: metrics, breaker: breaker}
}

func (t *providerMetricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	permit, allowed, retryAfter := t.breaker.allow(time.Now())
	if !allowed {
		seconds := int((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		body := `{"error":"provider circuit open"}`
		headers := make(http.Header)
		headers.Set("Content-Type", "application/json")
		headers.Set("Retry-After", strconv.Itoa(seconds))
		headers.Set("X-GemGate-Circuit", "open")
		return &http.Response{
			StatusCode:    http.StatusServiceUnavailable,
			Status:        fmt.Sprintf("%d %s", http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable)),
			Header:        headers,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       req,
		}, nil
	}

	start := time.Now()
	t.metrics.providerBegin(t.provider)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		providerFailure := !downstreamRequestAborted(req.Context())
		t.metrics.providerFinish(t.provider, 0, time.Since(start), providerFailure)
		t.breaker.finish(permit, providerFailure, time.Now())
		return nil, err
	}
	if resp.Body == nil {
		failed := resp.StatusCode >= 500
		t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
		t.breaker.finish(permit, failed, time.Now())
		return resp, nil
	}
	resp.Body = &providerMetricsBody{
		ReadCloser: resp.Body,
		done: func(bodyErr error) {
			transportError := bodyErr != nil && !downstreamRequestAborted(req.Context())
			t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), transportError)
			t.breaker.finish(permit, transportError || resp.StatusCode >= 500, time.Now())
		},
	}
	return resp, nil
}

type providerMetricsBody struct {
	io.ReadCloser
	once sync.Once
	done func(error)
}

func (b *providerMetricsBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		if err == io.EOF {
			b.finish(nil)
		} else {
			b.finish(err)
		}
	}
	return n, err
}

func (b *providerMetricsBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish(err)
	return err
}

func (b *providerMetricsBody) finish(err error) {
	b.once.Do(func() { b.done(err) })
}
