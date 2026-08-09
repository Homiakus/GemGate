package gateway

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var gatewayTracer = otel.Tracer("gemgate/gateway")

func startGatewaySpan(r *http.Request) (*http.Request, trace.Span) {
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := gatewayTracer.Start(ctx, "gemgate.request",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("url.path", r.URL.Path),
		),
	)
	return r.WithContext(ctx), span
}

func traceRequestID(ctx context.Context, requestID string) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("gemgate.request_id", requestID))
}

func traceAuth(ctx context.Context, domain, client string) {
	attrs := []attribute.KeyValue{attribute.String("gemgate.auth.domain", domain)}
	if client != "" {
		attrs = append(attrs, attribute.String("gemgate.client.name", client))
	}
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

func traceRateLimit(ctx context.Context, backend string, rpm int) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("gemgate.rate_limit.backend", backend),
		attribute.Int("gemgate.rate_limit.rpm", rpm),
	)
}

func traceProxyResult(ctx context.Context, result proxyResult, err error, clientAborted bool) {
	span := trace.SpanFromContext(ctx)
	if result.provider != "" {
		span.SetAttributes(attribute.String("gemgate.provider", result.provider))
	}
	if result.bytesOut > 0 {
		span.SetAttributes(attribute.Int64("http.response.body.size", result.bytesOut))
	}
	if result.status > 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", result.status))
	}
	if clientAborted {
		span.SetAttributes(attribute.String("gemgate.outcome", "client_cancelled"))
		return
	}
	if err != nil {
		span.SetAttributes(attribute.String("gemgate.outcome", "provider_error"))
		span.SetStatus(codes.Error, "provider request failed")
		return
	}
	if result.status >= 500 {
		span.SetStatus(codes.Error, "server error")
	}
}

func traceHTTPStatus(ctx context.Context, status int) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= 500 {
		span.SetStatus(codes.Error, "server error")
	}
}
