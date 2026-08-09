package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var providerTracer = otel.Tracer("gemgate/provider")

type providerMetricsTransport struct {
	provider  string
	base      http.RoundTripper
	metrics   *Metrics
	breaker   *circuitBreaker
	propagate bool
}

func newProviderMetricsTransport(provider string, base http.RoundTripper, metrics *Metrics, breaker *circuitBreaker, propagate ...bool) http.RoundTripper {
	if breaker == nil {
		breaker = newCircuitBreakerWithPolicy(defaultCircuitPolicy())
	}
	propagateUpstream := len(propagate) > 0 && propagate[0]
	return &providerMetricsTransport{provider: provider, base: base, metrics: metrics, breaker: breaker, propagate: propagateUpstream}
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

	ctx, span := providerTracer.Start(req.Context(), "gemgate.provider",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("gemgate.provider", t.provider),
			attribute.String("http.request.method", req.Method),
			attribute.String("server.address", req.URL.Hostname()),
		),
	)
	req = req.WithContext(ctx)
	if t.propagate {
		// Only W3C Trace Context is forwarded. Baggage and arbitrary inbound tracing
		// headers are deliberately excluded from the provider trust boundary.
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(req.Header))
	}

	start := time.Now()
	t.metrics.providerBegin(t.provider)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		providerFailure := !downstreamRequestAborted(req.Context())
		t.metrics.providerFinish(t.provider, 0, time.Since(start), providerFailure)
		t.breaker.finish(permit, providerFailure, time.Now())
		if providerFailure {
			span.SetAttributes(attribute.String("gemgate.outcome", "transport_error"))
			span.SetStatus(codes.Error, "provider transport error")
		} else {
			span.SetAttributes(attribute.String("gemgate.outcome", "client_cancelled"))
		}
		span.End()
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.Body == nil {
		failed := resp.StatusCode >= 500
		t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), false)
		t.breaker.finish(permit, failed, time.Now())
		if failed {
			span.SetStatus(codes.Error, "provider server error")
		}
		span.End()
		return resp, nil
	}
	resp.Body = &providerMetricsBody{
		ReadCloser: resp.Body,
		done: func(bodyErr error) {
			transportError := bodyErr != nil && !downstreamRequestAborted(req.Context())
			t.metrics.providerFinish(t.provider, resp.StatusCode, time.Since(start), transportError)
			t.breaker.finish(permit, transportError || resp.StatusCode >= 500, time.Now())
			switch {
			case transportError:
				span.SetAttributes(attribute.String("gemgate.outcome", "stream_error"))
				span.SetStatus(codes.Error, "provider stream error")
			case bodyErr != nil:
				span.SetAttributes(attribute.String("gemgate.outcome", "client_cancelled"))
			case resp.StatusCode >= 500:
				span.SetStatus(codes.Error, "provider server error")
			default:
				span.SetAttributes(attribute.String("gemgate.outcome", "completed"))
			}
			span.End()
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
