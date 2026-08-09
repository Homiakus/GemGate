package gateway

import (
	"io"
	"net/http"
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
	start := time.Now()
	t.metrics.providerBegin(t.provider)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.metrics.providerFinish(t.provider, 0, time.Since(start), true)
		return nil, err
	}
	if resp.Body == nil {
		t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
		return resp, nil
	}
	resp.Body = &providerMetricsBody{
		ReadCloser: resp.Body,
		done: func() {
			t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
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
