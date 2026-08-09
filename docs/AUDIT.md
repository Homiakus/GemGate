# Codebase audit — 2026-08

This document records the v0.2 → v0.4 architectural audit and the hardening work now merged directly into `main`.

## Summary

The original project was compact but encoded a single upstream assumption throughout config, gateway, CLI and TUI: **Gemini**. The first pass introduced a provider boundary. The next passes hardened production behavior around that boundary: credential isolation, strict config, rolling rate limits, configurable CORS, provider observability, modular provider-first TUI, atomic hot reload, file-backed secret rotation, provider circuit breaking and passive readiness.

The result is still intentionally a lightweight reverse proxy. It does not become a schema translator, workflow engine or retrying AI client.

## Findings

| Severity | Area | Finding | Status |
| --- | --- | --- | --- |
| High | Security | Client `Authorization` and provider credentials were handled by Gemini-specific gateway logic. | Fixed: sanitize inbound credentials and inject auth only after provider selection. |
| High | Architecture | `gateway.go` owned provider URL, auth and protocol decisions. | Fixed: provider catalog, named routing, provider transport boundary, modular gateway/runtime/proxy helpers. |
| High | Config | Only one `upstream` existed. | Fixed with backward-compatible `providers` normalization. |
| Medium | Config | YAML accepted unknown fields. | Fixed with `KnownFields(true)`. |
| Medium | Secrets | Provider/client credential rotation required restart. | Fixed: `api_key_file` / `token_file` plus atomic reload. |
| Medium | Runtime | Config updates risk partial live mutation if reload were added naively. | Fixed: load/validate/build complete candidate then atomic runtime snapshot swap. In-flight requests retain old snapshot. |
| Medium | HTTP | Detailed transport errors could escape to clients. | Fixed: generic public errors, detailed internal logs. |
| Medium | HTTP | Streaming copy errors could trigger a second response write. | Fixed with explicit response-started state. |
| Medium | HTTP | Hop-by-hop headers named by `Connection` were not dynamically filtered. | Fixed. |
| Medium | Operational | Private `/_healthz` could otherwise fall through as an application proxy path. | Fixed: health/readiness are always local endpoints; public when configured, bearer-protected otherwise. |
| Medium | Quality | No repository CI for formatting, vet, race tests and build. | Fixed. |
| Medium | Observability | Metrics were global and provider identity absent from the operator UI. | Fixed: provider-labelled metrics, passive health, provider-aware logs and Providers TUI view. |
| Medium | TUI | `internal/tui/model.go` was a large all-in-one file with Gemini-centric route labels. | Fixed: focused dashboard/logs/clients/providers/config/help/stats/helpers modules. |
| Medium | Rate limit | Fixed-window limiter permitted boundary bursts. | Fixed with exact rolling one-minute window. Cross-replica coordination remains open. |
| Medium | CORS | `Access-Control-Allow-Origin: *` was unconditional. | Fixed: configurable policy with allow-list, disable switch, preflight validation, credentials policy and max-age. |
| Medium | Resilience | Repeated transport/5xx failures could continuously hit a failing provider. | Fixed: provider circuit breaker with closed/open/half-open states and no automatic request replay. |
| Medium | Readiness | Liveness and provider availability were not separable. | Fixed: `/_healthz` liveness plus `/_readyz` passive readiness keyed to default-provider circuit state. |
| Low | Logging config | `log_body` and `log_headers` exist but do not drive logging behavior. | Open: implement only with explicit redaction rules or remove from public contract. |
| Low | Distributed control | Client RPM state is local to one process. | Open: optional shared backend for multi-replica deployments. |
| Low | Proxy trust | No trusted-proxy model exists. | Open before adding IP-derived client identity or policy. |
| Low | Tracing | No distributed tracing/OpenTelemetry. | Open, optional. |
| Low | Release | No reproducible release/provenance automation. | Open. |

## Current runtime architecture

Cross-cutting responsibilities now have separate boundaries:

- `internal/config` — strict parsing, defaults, environment and file-backed secrets, validation;
- `internal/provider` — provider metadata/auth;
- `gateway.go` — request/server coordination;
- `runtime.go` — immutable runtime state and atomic swap;
- `proxy.go` — routing/auth/upstream flow;
- `circuitbreaker.go` — resilience state machine;
- `provider_metrics_transport.go` — stream-aware metrics/circuit enforcement;
- `health.go` / `readiness.go` — local operational health semantics;
- `http_helpers.go` — shared HTTP safety helpers;
- `internal/tui` — presentation only.

Provider-specific payload translation remains outside the gateway by design.

## Hot reload result

The reload path is validation-before-swap:

1. re-read `config.yaml`;
2. re-resolve environment placeholders and referenced secret files;
3. apply defaults;
4. validate all providers, clients, CORS, URLs and durations;
5. build all provider clients/auth maps/rate-limit state for a complete candidate runtime;
6. reject the entire revision on any error;
7. atomically replace the active snapshot only on success.

Already running requests retain their previous snapshot to completion, including streaming responses. New requests see the new snapshot immediately after swap.

Reloadable: providers/default provider, provider credentials/URL/headers/timeouts, clients/tokens/enabled/RPM, CORS, request-body limit and log ring size.

Restart-required: listen/read/write/idle server settings.

## Circuit/readiness result

Circuit behavior currently uses conservative built-in defaults:

- 5 consecutive transport/5xx failures;
- 30 second open interval;
- one half-open request after cooldown;
- 4xx/429 do not trip;
- open requests return local 503 and never call upstream;
- no automatic retries.

`/_readyz` is passive and quota-free. The default provider circuit gates process readiness; a non-default provider outage does not remove the entire gateway from readiness.

## Verification

The repository CI checks every pushed revision with:

```text
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

The atomic hot-reload layer and the subsequent circuit-breaker/readiness/modular-gateway revision both passed the complete pipeline on `main`.

## Recommended next iteration

1. Add optional shared/distributed rate-limit state for multi-replica deployments.
2. Add trusted-proxy configuration before introducing IP-derived policy/audit identity.
3. Add optional OpenTelemetry traces while keeping request/response bodies disabled by default.
4. Decide whether `logging.log_body` / `logging.log_headers` should be implemented with strong field-level redaction or removed.
5. Add broader provider-shaped integration tests for cancellation, timeouts, SSE edge cases and connection failures.
6. Make circuit-breaker thresholds/open interval optionally configurable while retaining conservative defaults.
7. Expose circuit state explicitly in the TUI Providers screen and Prometheus series.
8. Add release automation, changelog/version metadata, SBOM and reproducible binary/container provenance.
