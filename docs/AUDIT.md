# Codebase audit — 2026-08

This document records the main findings from the v0.2 → v0.3 audit and the hardening passes that followed. It separates defects addressed in the current branch from remaining work so architectural debt is visible rather than hidden behind a refactor.

## Summary

The original project was compact and readable, but the architecture encoded a single assumption throughout the config, gateway, CLI and TUI: **the upstream is Gemini**. That made every new provider a likely cross-cutting change. The highest-value change was therefore not adding more `if provider == ...` branches, but introducing a provider catalog and a stable routing/auth boundary.

The second hardening pass focused on production behavior around that boundary: browser access policy, per-client burst behavior, and provider-level observability.

## Findings

| Severity | Area | Finding | Status |
| --- | --- | --- | --- |
| High | Security | Client `Authorization` and provider credential headers were handled by Gemini-specific logic in the gateway. Adding providers would risk credential leakage/override. | Fixed: sanitize inbound credentials, inject auth in provider adapter. |
| High | Architecture | `gateway.go` owned provider URL, auth and protocol decisions directly. | Fixed: provider catalog + named routing. |
| High | Config | Only one `upstream` existed; provider growth required schema replacement. | Fixed with backward-compatible `providers` normalization. |
| Medium | Config | YAML parser accepted unknown fields, so typos could silently fall back to defaults. | Fixed with `KnownFields(true)`. |
| Medium | HTTP | Proxy errors could expose detailed upstream transport errors to clients. | Fixed: generic public errors, detailed in internal log entry. |
| Medium | HTTP | A streaming copy error after response headers were sent could lead to a second `http.Error` write attempt. | Fixed with explicit `responseStarted` state. |
| Medium | HTTP | Hop-by-hop headers named by the `Connection` header were not dynamically filtered. | Fixed. |
| Medium | Docs | README referenced `config.example.yaml`, but that file was absent. | Fixed. |
| Medium | Quality | No repository CI for gofmt/vet/race tests/build. | Fixed. |
| Medium | Observability | Log records had no provider identity and metrics were only global. | Fixed in gateway: provider identity, per-provider request/status/transport/in-flight/duration metrics and passive health state. TUI presentation remains follow-up. |
| Medium | TUI | `internal/tui/model.go` is a large all-in-one view/state/statistics file and still presents Gemini-centric route labels. | Follow-up. |
| Medium | Rate limit | Fixed-window limiter allowed boundary bursts and was in-memory/per process. | Boundary burst fixed with exact one-minute sliding window. Cross-replica coordination remains follow-up. |
| Medium | CORS | `Access-Control-Allow-Origin: *` was unconditional. | Fixed: configurable middleware with allow-list, disable switch, preflight validation, credentials policy and max-age. |
| Low | Logging config | `log_body` and `log_headers` are config fields but do not drive logging behavior. | Follow-up: implement safely or remove until designed. |
| Low | Runtime config | No hot reload; key rotation requires restart. | Follow-up. |
| Low | Resilience | No provider-level circuit breaker, retry policy or active health probe. | Partially improved with passive provider health state. Circuit breaking/retries remain follow-up; retries must be endpoint/idempotency aware. |

## Architectural result

The provider-specific surface is intentionally small:

- metadata/default URL;
- auth mode;
- default headers;
- whether an API key is mandatory;
- whether the endpoint is OpenAI-compatible.

The gateway remains payload-agnostic. Translating OpenAI ↔ Anthropic ↔ Gemini schemas in the proxy core would create a much larger correctness surface and couple GemGate to fast-changing provider schemas.

Cross-cutting HTTP concerns now sit outside provider protocol logic:

- CORS is handled as middleware before the gateway handler;
- rate limiting uses a per-client sliding window;
- provider metrics are attached at the provider transport boundary, so streaming duration and in-flight state are measured without provider-specific payload parsing.

## Recommended next iteration

1. Split TUI into `dashboard`, `logs`, `clients`, `providers`, `config` view modules and make provider traffic/health first-class.
2. Add atomic config reload with validation-before-swap and secret rotation.
3. Add optional Redis-backed rate-limit coordination for multi-replica deployments.
4. Add trusted-proxy configuration before introducing any IP-derived policy.
5. Add optional OpenTelemetry traces; keep request/response bodies off by default.
6. Add a provider circuit breaker and optional active readiness probe without blind retries of non-idempotent generation requests.
7. Decide whether `logging.log_body` / `log_headers` should be implemented with explicit redaction rules or removed from the public config contract.
