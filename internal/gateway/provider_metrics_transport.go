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
}

func newProviderMetricsTransport(provider string, base http.RoundTripper, metrics *Metrics) http.RoundTripper {
	return &providerMetricsTransport{provider: provider, base: base, metrics: metrics}
}

func (t *providerMetricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	breaker := t.metrics.circuit(t.provider)
	permit, allowed, retryAfter := breaker.allow(time.Now())
	if !allowed {
		seconds := int((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		body := `{"error":"provider circuit open"}`
		return &http.Response{
			StatusCode:    http.StatusServiceUnavailable,
			Status:        fmt.Sprintf("%d %s", http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable)),
			Header:        http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{strconv.Itoa(seconds)}, "X-GemGate-Circuit": []string{"open"}},
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       req,
		}, nil
	}

	start := time.Now()
	t.metrics.providerBegin(t.provider)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.metrics.providerFinish(t.provider, 0, time.Since(start), true)
		breaker.finish(permit, true, time.Now())
		return nil, err
	}
	if resp.Body == nil {
		failed := resp.StatusCode >= 500
		t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
		breaker.finish(permit, failed, time.Now())
		return resp, nil
	}
	resp.Body = &providerMetricsBody{
		ReadCloser: resp.Body,
		done: func() {
			t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
			breaker.finish(permit, resp.StatusCode >= 500, time.Now())
		},
	}
	return resp, nil
}

type providerMetricsBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *providerMetricsBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *providerMetricsBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish()
	return err
}

func (b *providerMetricsBody) finish() {
	b.once.Do(b.done)
}
