package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gemgate/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingDoesNotCaptureSensitiveRequestData(t *testing.T) {
	exporter, cleanup := installTestTracer(t)
	defer cleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-secret"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/responses?private_query=query-secret", strings.NewReader(`{"prompt":"body-secret"}`))
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("X-Private-Header", "header-secret")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	spans := exporter.GetSpans()
	if len(spans) < 2 {
		t.Fatalf("expected server and provider spans, got %d", len(spans))
	}
	for _, span := range spans {
		for _, kv := range span.Attributes {
			if kv.Value.Type() != attribute.STRING {
				continue
			}
			value := kv.Value.AsString()
			for _, secret := range []string{"query-secret", "body-secret", "client-token", "provider-secret", "header-secret"} {
				if strings.Contains(value, secret) {
					t.Fatalf("span %q attribute %q leaked %q: %s", span.Name, kv.Key, secret, value)
				}
			}
		}
	}
}

func TestTracingHeadersAreNotForwardedByDefault(t *testing.T) {
	_, cleanup := installTestTracer(t)
	defer cleanup()

	var traceparent, baggage string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("Traceparent")
		baggage = r.Header.Get("Baggage")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("Baggage", "private=do-not-forward")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	if traceparent != "" || baggage != "" {
		t.Fatalf("tracing metadata crossed provider boundary by default: traceparent=%q baggage=%q", traceparent, baggage)
	}
}

func TestTraceContextPropagationIsExplicitAndBaggageFree(t *testing.T) {
	_, cleanup := installTestTracer(t)
	defer cleanup()

	var traceparent, baggage string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("Traceparent")
		baggage = r.Header.Get("Baggage")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Telemetry.Enabled = true
	rt.Config.Telemetry.PropagateUpstream = true
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Baggage", "private=do-not-forward")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	if traceparent == "" {
		t.Fatal("expected W3C traceparent when propagation is explicitly enabled")
	}
	if baggage != "" {
		t.Fatalf("baggage must never cross provider boundary, got %q", baggage)
	}
}

func installTestTracer(t *testing.T) (*tracetest.InMemoryExporter, func()) {
	t.Helper()
	oldProvider := otel.GetTracerProvider()
	oldPropagator := otel.GetTextMapPropagator()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return exporter, func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldPropagator)
	}
}

func formatSpanAttributes(attrs []attribute.KeyValue) string {
	var b strings.Builder
	for _, kv := range attrs {
		fmt.Fprintf(&b, "%s=%v;", kv.Key, kv.Value)
	}
	return b.String()
}
