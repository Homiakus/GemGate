# Codebase audit — 2026-08

This document records the architectural audit and hardening work merged directly into `main`.

## Summary

GemGate started as a compact Gemini-specific proxy. It is now a provider-first AI gateway with strict config, credential isolation, multi-provider routing, atomic hot reload, file-backed secrets, dedicated operations auth, exact rolling limits, Redis-distributed quota with tested Sentinel failover, configurable CORS, trusted-proxy handling, provider observability, circuit breaking, passive readiness, privacy-bounded OpenTelemetry tracing, modular TUI, cross-platform release packaging and provider-shaped integration tests.

The project deliberately remains a reverse proxy. It does not translate provider schemas, hide billable retries, or capture prompts/completions for logging or tracing.

## Findings

| Severity | Area | Finding | Status |
| --- | --- | --- | --- |
| High | Security | Client and provider credentials were coupled to Gemini-specific proxy logic. | Fixed: inbound provider credentials are stripped; selected provider auth is injected server-side. |
| High | Architecture | One `upstream` was embedded across config/gateway/UI. | Fixed: provider catalog, named routing and backward-compatible legacy normalization. |
| High | Control plane | Any application client token could read protected metrics/config operational surfaces. | Fixed: optional dedicated operations token separates control-plane endpoints from application proxy credentials while preserving legacy fallback when omitted. |
| Medium | Config | Unknown YAML fields were silently accepted. | Fixed with strict `KnownFields(true)` plus strict telemetry subfield validation. |
| Medium | Secrets | Key/token rotation required restart. | Fixed for provider/client/operations credentials with file-backed secrets and atomic reload. |
| Medium | Runtime | Live config could have been partially mutated. | Fixed: validate/build complete candidate, then atomic snapshot swap. |
| Medium | HTTP | Public errors could reveal transport detail or double-write after streaming began. | Fixed: generic client errors plus response-start tracking. |
| Medium | HTTP | Hop-by-hop/forwarding headers were insufficiently sanitized. | Fixed: dynamic `Connection` filtering, trusted forwarding reconstruction and explicit tracing-header boundary. |
| Medium | Redirect security | Provider HTTP redirects could trigger hidden follow-up requests with server-side credentials. | Fixed: provider clients never auto-follow redirects; `3xx + Location` passes through to the caller. |
| Medium | Quality | No complete repository CI. | Fixed: module verification, gofmt diagnostics, vet, race/coverage tests, build, Redis service integration, Sentinel promotion E2E and release-packaging smoke. |
| Medium | TUI | Large Gemini-centric model/UI. | Fixed: focused views including provider/circuit/rate-limit/operations-auth/tracing state. |
| Medium | Rate limit | Fixed window allowed boundary bursts. | Fixed: exact rolling one-minute in-memory window. |
| Medium | Distributed quota | Client RPM state was local to one process. | Fixed: optional Redis backend shares one rolling quota across replicas; memory remains default. |
| Medium | Distributed security | Shared limiter could expose client tokens in external keys. | Fixed: Redis keys use a truncated SHA-256 token identifier; raw bearer tokens are not key material. |
| Medium | Distributed failure policy | External limiter outage semantics were undefined. | Fixed: fail-closed by default, explicit `fail_open`, warning logs and backend-error metric. |
| Medium | Redis HA | A shared Redis limiter could still be pinned to one Redis master. | Fixed and E2E-tested: URLs carrying `master_name` select go-redis Sentinel failover mode; forced promotion preserves limiter continuity through the same failover client. |
| Medium | CORS | Wildcard origin was unconditional. | Fixed: allow-list, disable switch, preflight validation, credentials policy and max-age. |
| Medium | Proxy trust | Forwarded client IP could be spoofed if consumed naively. | Fixed: explicit trusted CIDR/IP chain and sanitized upstream forwarding headers. |
| Medium | Resilience | Repeated transport/5xx failures continuously hit a failing provider. | Fixed: configurable closed/open/half-open circuit breaker with no automatic request replay. |
| Medium | Readiness | Process liveness and provider readiness were coupled. | Fixed: `/_healthz` liveness plus passive `/_readyz` based on default-provider circuit state. |
| Medium | Cancellation | Downstream cancellation could be misclassified as provider failure. | Fixed and integration-tested; provider timeout remains a provider failure. |
| Medium | Streaming accounting | Upstream could return `200` and then truncate the body while provider metrics/circuit state recorded success. | Fixed: non-EOF stream read failures are transport/circuit failures unless the downstream caller cancelled. |
| Medium | Release | No repeatable multi-platform release/provenance path. | Fixed: shared build script, six targets, version injection, checksums, SPDX SBOM and GitHub artifact attestations on version tags. |
| Low | Logging config | `log_body` / `log_headers` existed but did nothing. | Fixed: legacy false values remain compatible; true is explicitly rejected until a redaction contract exists. |
| Low | Tracing | No distributed tracing/OpenTelemetry. | Fixed: optional OTLP/HTTP request/provider spans with strict metadata-only privacy boundary and explicit upstream Trace Context propagation. |
| Low | Redis Cluster | Native Redis Cluster routing is not implemented. | Open by design; Sentinel failover and stable managed-primary endpoints are supported. |
| Low | Streaming edges | Large SSE flush, unknown-length body limit, truncated fixed-length bodies and malformed chunked disconnects are covered. | Mostly fixed; provider-specific exotic framing/teardown cases can still be expanded. |

## Current runtime boundaries

- `internal/config` — strict parsing, defaults, secret resolution, operations auth, telemetry policy, trusted proxies, Redis/circuit validation and legacy migration;
- `internal/provider` — provider metadata and authentication contract;
- `internal/telemetry` — process-scoped OpenTelemetry SDK/OTLP bootstrap;
- `internal/gateway/runtime.go` — immutable request runtime and atomic reload;
- `internal/gateway/operations_auth.go` — control-plane authentication boundary;
- `internal/gateway/tracing.go` — bounded inbound request span metadata;
- `internal/gateway/proxy.go` — routing/auth/upstream streaming and redacted config view;
- `internal/gateway/ratelimit*.go` — memory/Redis/Sentinel rolling quota backends;
- `internal/gateway/circuitbreaker.go` — snapshot-scoped provider resilience;
- `internal/gateway/provider_metrics_transport.go` — stream-aware provider metrics, circuit enforcement and provider spans;
- `internal/gateway/trusted_proxy.go` — client IP trust boundary;
- `internal/gateway/health.go` / `readiness.go` — local operational endpoints;
- `internal/tui` — presentation only.

## OpenTelemetry result

OpenTelemetry traces use OTLP/HTTP and are process-scoped/restart-only. Request spans record method, path without query, request ID, auth domain/client name, rate-limit policy, selected provider, status and normalized outcome. Provider spans stay alive through response-body EOF/Close so streaming duration is measured instead of only time-to-headers.

Regression tests verify that query values, request bodies, application tokens, provider keys and arbitrary headers do not appear in span attributes. Incoming tracing headers are stripped at the provider boundary. Upstream propagation is disabled by default; when explicitly enabled only W3C Trace Context is injected. Baggage is never forwarded. Redacted operator surfaces expose only safe telemetry state and never the collector endpoint or exporter credentials.

See `docs/OBSERVABILITY.md`.

## Operations-auth result

With `operations.token` or `operations.token_file` configured, `/_metrics`, `/_config` and private health/readiness require the dedicated operations bearer token. Application tokens cannot use those surfaces; the operations token cannot proxy AI requests. Token rotation is hot-reloadable through the same complete runtime swap.

When `operations:` is omitted, historical client-token access to protected operational endpoints remains available for compatibility. Production deployments should opt into dedicated operations auth.

See `docs/OPERATIONS.md`.

## Distributed rate-limit result

`memory` remains the zero-dependency default. `redis` is opt-in for horizontally scaled deployments. The Redis path uses one atomic Lua operation and Redis server time. Normal CI starts a real Redis service and verifies under the race detector that independent limiter instances observe one shared quota.

When the secret Redis URL includes `master_name`, GemGate constructs a native go-redis Sentinel failover client instead of a fixed standalone client. Repeated `addr=` query values provide additional Sentinel seeds. `/_config` and TUI expose only `standalone`/`sentinel` mode, not Redis addresses or credentials. Stable managed-primary endpoints continue to use normal Redis mode.

The dedicated Sentinel E2E workflow starts a master, replica and three Sentinel processes, records limiter state, forces `SENTINEL FAILOVER`, waits for a different master, then verifies that the same GemGate failover client reconnects and sees the pre-promotion quota state.

See `docs/RATE_LIMITING.md`.

## Release result

Normal CI exercises the same cross-platform packaging script used by tag releases. Version tags build Linux/macOS/Windows for amd64/arm64, generate SHA-256 checksums and SPDX SBOM, create GitHub artifact attestations, and publish release assets.

See `docs/RELEASING.md`.

## Verification

Every pushed revision is checked with:

```text
go mod verify
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

CI additionally starts Redis and exercises release cross-compilation/version injection. Tracing regression tests enforce the no-secrets/no-body/no-query span contract and opt-in upstream propagation. Redis unit tests verify standalone/Sentinel URL selection and reject malformed failover options before serving requests. The separate Sentinel E2E workflow verifies forced master promotion and limiter-state continuity.

## Recommended next iteration

1. Expand provider-specific streaming/framing and connection-teardown integration tests beyond the generic malformed chunked case.
2. Consider native Redis Cluster routing only if a deployment actually needs sharded limiter storage.
3. Consider OTLP metrics/exemplars only if they add operational value beyond the existing Prometheus surface.
4. Consider a separate operations listener only if deployments need network-level control-plane isolation beyond token auth.
