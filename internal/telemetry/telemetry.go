package telemetry

import (
	"context"
	"fmt"

	"gemgate/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type ShutdownFunc func(context.Context) error

func Setup(ctx context.Context, cfg config.TelemetryConfig, version string) (ShutdownFunc, error) {
	normalized, err := cfg.Normalized()
	if err != nil {
		return nil, err
	}
	if !normalized.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	opts := make([]otlptracehttp.Option, 0, 1)
	if normalized.Endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(normalized.Endpoint))
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP HTTP trace exporter: %w", err)
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", normalized.ServiceName),
	}
	if version != "" {
		attrs = append(attrs, attribute.String("service.version", version))
	}
	if normalized.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", normalized.Environment))
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attrs...)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(normalized.SampleRatio))),
	)
	otel.SetTracerProvider(provider)
	// Trace Context only. Baggage is deliberately not accepted or forwarded because
	// arbitrary baggage values can contain user/application data.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return provider.Shutdown, nil
}
