# Codebase audit — 2026-08

This document records the architectural audit and hardening work merged directly into `main`.

## Summary

GemGate started as a compact Gemini-specific proxy. The current architecture is provider-first and production-oriented: strict config, credential isolation, multi-provider routing, atomic hot reload, file-backed secrets, exact rolling limits, optional Redis-distributed quota, configurable CORS, trusted-proxy handling, provider observability, circuit breaking, passive readiness, modular TUI and provider-shaped integration tests.

The project deliberately remains a reverse proxy. It does not translate provider schemas, hide billable retries, or capture prompts/completions for logging.

## Findings

| Severity | Area | Finding | Status |
| --- | --- | --- | --- |
| High | Security | Client and provider credentials were coupled to Gemini-specific proxy logic. | Fixed: inbound provider credentials are stripped; selected provider auth is injected server-side. |
| High | Architecture | One `upstream` was embedded across config/gateway/UI. | Fixed: provider catalog, named routing and backward-compatible legacy normalization. |
| Medium | Config | Unknown YAML fields were silently accepted. | Fixed with strict `KnownFields(true)`. |
| Medium | Secrets | Key/token rotation required restart. | Fixed for provider/client credentials with `api_key_file`, `token_file` and atomic reload. |
| Medium | Runtime | Live config could have been partially mutated. | Fixed: validate/build complete candidate, then atomic snapshot swap. |
| Medium | HTTP | Public errors could reveal transport detail or double-write after streaming began. | Fixed: generic client errors plus response-start tracking. |
| Medium | HTTP | Hop-by-hop/forwarding headers were insufficiently sanitized. | Fixed: dynamic `Connection` filtering and trusted forwarding reconstruction. |
| Medium | Quality | No complete repository CI. | Fixed: module verification, exact gofmt diagnostics, vet, race/coverage tests, build and real Redis service integration. |
| Medium | TUI | Large Gemini-centric model/UI. | Fixed: focused views including provider/circuit/rate-limit state. |
| Medium | Rate limit | Fixed window allowed boundary bursts. | Fixed: exact rolling one-minute in-memory window. |
| Medium | Distributed quota | Client RPM state was local to one process. | Fixed: optional Redis backend shares one rolling quota across replicas; memory remains default. |
| Medium | Distributed security | Shared limiter could expose client tokens in external keys. | Fixed: Redis keys use a truncated SHA-256 token identifier; raw bearer tokens are not key material. |
| Medium | Distributed failure policy | External limiter outage semantics were undefined. | Fixed: fail-closed by default, explicit `fail_open`, warning logs and backend-error metric. |
| Medium | CORS | Wildcard origin was unconditional. | Fixed: allow-list, disable switch, preflight validation, credentials policy and max-age. |
| Medium | Proxy trust | Forwarded client IP could be spoofed if consumed naively. | Fixed: explicit trusted CIDR/IP chain and sanitized upstream forwarding headers. |
| Medium | Resilience | Repeated transport/5xx failures continuously hit a failing provider. | Fixed: configurable closed/open/half-open circuit breaker with no automatic request replay. |
| Medium | Readiness | Process liveness and provider readiness were coupled. | Fixed: `/_healthz` liveness plus passive `/_readyz` based on default-provider circuit state. |
| Medium | Cancellation | Downstream cancellation could be misclassified as provider failure. | Fixed and integration-tested; provider timeout remains a provider failure. |
| Low | Logging config | `log_body` / `log_headers` existed but did nothing. | Fixed: legacy false values remain compatible; true is explicitly rejected until a redaction contract exists. |
| Low | Tracing | No distributed tracing/OpenTelemetry. | Open, optional. |
| Low | Release | No reproducible release/provenance automation. | Open. |

## Current runtime boundaries

- `internal/config` — strict parsing, defaults, secret resolution, trusted proxies, Redis/circuit validation and legacy migration;
- `internal/provider` — provider metadata and authentication contract;
- `internal/gateway/runtime.go` — immutable request runtime and atomic reload;
- `internal/gateway/proxy.go` — routing/auth/upstream streaming and redacted config view;
- `internal/gateway/ratelimit*.go` — memory/Redis rolling quota backends;
- `internal/gateway/circuitbreaker.go` — snapshot-scoped provider resilience;
- `internal/gateway/provider_metrics_transport.go` — stream-aware provider metrics/circuit enforcement;
- `internal/gateway/trusted_proxy.go` — client IP trust boundary;
- `internal/gateway/health.go` / `readiness.go` — local operational endpoints;
- `internal/tui` — presentation only.

## Distributed rate-limit result

`memory` remains the zero-dependency default. `redis` is opt-in for horizontally scaled deployments.

The Redis path uses one atomic Lua operation and Redis server time. CI starts a real Redis service and verifies under the race detector that two independent limiter instances observe one quota for the same client while different tokens remain isolated.

The Redis URL can be supplied through `url_file`. Redacted config/TUI surfaces expose backend state but never the URL/password. Backend infrastructure settings require restart; per-client RPM/token policy remains hot-reloadable.

See `docs/RATE_LIMITING.md`.

## Verification

Every pushed revision is checked with:

```text
go mod verify
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

CI additionally starts Redis and exercises the shared-quota path rather than mocking it.

## Recommended next iteration

1. Add optional OpenTelemetry traces/metrics export without request/response body capture.
2. Add reproducible release automation: version injection, checksums, SBOM and provenance.
3. Add deployment examples for Redis HA/Sentinel/managed Redis if operational demand appears.
4. Expand provider-shaped integration cases for unusual streaming framing and connection teardown.
