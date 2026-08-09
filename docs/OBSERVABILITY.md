# Observability

GemGate exposes two complementary observability surfaces:

- Prometheus metrics on `/_metrics` for aggregate process/provider/rate-limit/circuit state;
- optional OpenTelemetry traces over OTLP/HTTP for request-to-provider latency and failure correlation.

OpenTelemetry tracing is opt-in and deliberately metadata-only.

## OTLP/HTTP tracing

```yaml
telemetry:
  enabled: true
  service_name: "gemgate"
  endpoint: "http://otel-collector:4318/v1/traces"
  sample_ratio: 0.10
  environment: "production"
  propagate_upstream: false
```

`endpoint` is the complete trace endpoint, including `/v1/traces`. It may be omitted when the exporter is configured with standard OpenTelemetry environment variables such as `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.

Collector authentication belongs in exporter environment variables/secret injection, for example `OTEL_EXPORTER_OTLP_TRACES_HEADERS`. GemGate does not add a telemetry-header secret field to YAML.

Telemetry settings are process-scoped and require restart. A hot-reload candidate that changes telemetry settings is rejected as a whole rather than partially applying a new exporter or sampler.

## Sampling

`sample_ratio` is in the inclusive range `(0, 1]` when telemetry is enabled. Disable telemetry instead of setting zero.

GemGate uses a parent-based trace-ID-ratio sampler. An accepted W3C remote parent therefore remains part of the same distributed trace while root requests are sampled by the configured ratio.

## Spans

GemGate produces two principal span kinds:

### `gemgate.request`

Server span for the request received by GemGate. Metadata includes only bounded operational fields such as:

- HTTP method;
- URL path **without query string**;
- request ID;
- auth domain and configured client name after successful application auth;
- configured rate-limit backend/RPM;
- selected provider;
- final HTTP/provider status and response byte count where available;
- normalized outcome such as provider error or client cancellation.

### `gemgate.provider`

Client span around the selected upstream provider request. It stays open until the provider response body reaches EOF/Close, so long-lived SSE/model streams measure full streaming lifetime rather than only time-to-headers.

Metadata includes provider name, HTTP method, provider hostname, status, and a normalized outcome. Transport/stream failures mark the span as error. Downstream cancellation is classified separately.

## Privacy boundary

Traces intentionally do **not** capture:

- request or response bodies;
- prompts or model completions;
- URL query strings;
- bearer tokens;
- provider API keys;
- arbitrary request/response headers;
- Redis connection URLs;
- raw transport error strings.

This is enforced by regression tests. Sensitive body/header logging remains unavailable separately from tracing.

## Trace propagation to providers

Inbound `traceparent`, `tracestate`, and `baggage` headers are stripped before the provider trust boundary.

By default:

```yaml
telemetry:
  propagate_upstream: false
```

No tracing metadata is sent to the AI provider.

If `propagate_upstream: true` is explicitly configured, GemGate injects **W3C Trace Context only** for the provider child span. Baggage is never forwarded. This keeps arbitrary application baggage outside provider requests while still allowing explicit end-to-end trace correlation for providers or private OpenAI-compatible services that participate in W3C tracing.

## Operational endpoints

`/_metrics` continues to expose Prometheus text metrics and is independent from OTLP tracing. Enabling OpenTelemetry does not replace the existing metrics surface.

When a dedicated `operations` token is configured, `/_metrics` and `/_config` use the operations credential rather than application tokens.

## Collector deployment

For production, send OTLP to an OpenTelemetry Collector or an OTLP-compatible managed endpoint rather than coupling GemGate to a backend-specific tracing protocol. Keep the collector endpoint on a trusted network or TLS-protected connection and inject collector credentials through environment/orchestrator secrets.
