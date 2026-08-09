# Codebase audit — 2026-08

This document records the main findings from the v0.2 → v0.3 audit and the hardening passes applied in PR #1. It separates completed work from remaining architectural debt.

## Summary

The original project was compact and readable, but config, gateway, CLI and TUI all encoded one assumption: **the upstream is Gemini**. That made provider growth a cross-cutting change. The first priority was therefore a stable provider boundary rather than more provider-specific branches.

The next pass hardened production behavior around that boundary: browser policy, client burst control, provider-level observability, and a provider-first TUI. The former all-in-one TUI model was also split into focused modules so UI work no longer requires editing one very large file.

## Findings

| Severity | Area | Finding | Status |
| --- | --- | --- | --- |
| High | Security | Client `Authorization` and provider credentials were handled by Gemini-specific gateway logic. | Fixed: sanitize inbound credentials and inject auth only after provider selection. |
| High | Architecture | `gateway.go` owned provider URL, auth and protocol decisions. | Fixed: provider catalog + named routing + provider transport boundary. |
| High | Config | Only one `upstream` existed. | Fixed with backward-compatible `providers` normalization. |
| Medium | Config | YAML accepted unknown fields. | Fixed with `KnownFields(true)`. |
| Medium | HTTP | Detailed transport errors could escape to clients. | Fixed: generic public error, detailed internal log entry. |
| Medium | HTTP | Streaming copy errors could trigger a second response write. | Fixed with explicit response-started state. |
| Medium | HTTP | Hop-by-hop headers named by `Connection` were not dynamically filtered. | Fixed. |
| Medium | Docs | README referenced a missing `config.example.yaml`. | Fixed. |
| Medium | Quality | No repository CI for formatting, vet, race tests and build. | Fixed. |
| Medium | Observability | Metrics were global and provider identity was absent from the operational UI. | Fixed: provider-labelled metrics, passive health, provider log identity and provider-first TUI. |
| Medium | TUI | `internal/tui/model.go` was a large all-in-one state/view/stats file with Gemini-centric route labels. | Fixed: core model plus focused dashboard/logs/clients/providers/config/help/stats/helpers modules. |
| Medium | Rate limit | Fixed-window limiter permitted boundary bursts. | Fixed with exact rolling one-minute window. Cross-replica coordination remains open. |
| Medium | CORS | `Access-Control-Allow-Origin: *` was unconditional. | Fixed: configurable middleware with allow-list, disable switch, preflight validation, credentials policy and max-age. |
| Low | Logging config | `log_body` and `log_headers` exist but do not drive logging behavior. | Open: implement only with explicit redaction rules, or remove from public contract. |
| Low | Runtime config | Key/config rotation requires restart. | Open: atomic validation-before-swap reload. |
| Low | Resilience | No circuit breaker or active readiness probe. | Partially improved with passive health; circuit state remains open. |
| Low | Distributed control | Client RPM state is local to one process. | Open: optional shared backend for multi-replica deployments. |

## Architectural result

Provider-specific behavior is intentionally small:

- metadata/default URL;
- auth mode;
- default headers;
- whether an API key is mandatory;
- whether the endpoint is OpenAI-compatible.

The proxy remains payload-agnostic. OpenAI ↔ Anthropic ↔ Gemini schema translation does **not** belong in the gateway core.

Cross-cutting concerns now have separate boundaries:

- CORS middleware runs before the gateway handler;
- client rate limiting is independent of provider protocol;
- provider metrics wrap each provider transport and include the streamed response lifecycle;
- TUI consumes snapshots and does not own provider protocol logic.

## TUI result

The TUI is split into focused files:

- `model.go` — Bubble Tea state, input and refresh lifecycle;
- `dashboard.go` — global traffic + provider health summary;
- `logs.go` — provider-aware request log;
- `clients.go` — client usage and limits;
- `providers.go` — provider state, health and traffic;
- `config_view.go` — redacted runtime config and CORS state;
- `help_view.go` — operator help;
- `stats.go` / `helpers.go` — aggregation and presentation helpers.

This is deliberately a presentation split rather than a new framework inside the TUI.

## Current verification

GitHub Actions runs on push and pull request and checks:

```text
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

The multi-provider core, hardening pass, and provider-first TUI all passed this pipeline before the documentation sync commit.

## Recommended next iteration

1. Add atomic config reload with validation-before-swap and provider/client secret rotation.
2. Add optional shared rate-limit state for multi-replica deployments.
3. Add trusted-proxy configuration before introducing IP-derived policy or audit identity.
4. Add optional OpenTelemetry traces while keeping request/response bodies off by default.
5. Add a provider circuit breaker and optional active readiness probes without blind retries of non-idempotent generation requests.
6. Decide whether `logging.log_body` / `logging.log_headers` should be implemented with strong redaction rules or removed.
7. Add integration tests against provider-shaped local stubs for streaming, cancellation, timeout, CORS and circuit-state scenarios.
8. Add release automation, changelog/version metadata, and reproducible binaries/container provenance.
