# Security policy and deployment notes

## Reporting vulnerabilities

Please report security-sensitive issues privately to the repository owner rather than opening a public issue containing credentials, exploit details, production topology, or reproducible attack steps against a live deployment.

## Trust boundaries

GemGate supports three independent credential classes:

1. application tokens (`clients[].token` / `clients[].token_file`) authorize proxied AI requests;
2. operations token (`operations.token` / `operations.token_file`) protects operational endpoints;
3. provider credentials (`providers[].api_key` / `providers[].api_key_file` plus server-side provider headers) authenticate GemGate to upstream AI services.

A configured operations token must differ from every enabled application token. It is stored outside the application token map and cannot proxy provider requests. Conversely, application tokens cannot access protected operational endpoints once dedicated operations auth is enabled.

Incoming provider-auth headers are stripped before the selected provider adapter reconstructs upstream authentication from server-side configuration.

## Operations control plane

Production deployments should use both credential and network isolation:

```yaml
operations:
  token_file: "/run/secrets/gemgate_operations_token"
```

```bash
gemgate serve -config config.yaml -operations-listen 127.0.0.1:9090
```

When `-operations-listen` is set:

- `/_healthz`, `/_readyz`, `/_metrics`, and `/_config` return 404 on the application listener;
- application/provider paths return 404 on the operations listener;
- handler isolation is installed before either listener starts accepting requests;
- the operations listener can be restricted independently with firewall/security-group/NetworkPolicy controls.

`public_health: true` only makes health/readiness unauthenticated on the operations handler when listener isolation is enabled; it does not re-expose them on the application port.

If `operations:` or `-operations-listen` are omitted, legacy behavior remains for backward compatibility. Treat those fallbacks as compatibility modes rather than the preferred production posture.

See `docs/OPERATIONS.md`.

## Secret delivery and rotation

Prefer environment/orchestrator credentials or file-backed secrets over embedding credentials directly in `config.yaml`.

Supported file-backed secrets include provider keys, application tokens, operations token and Redis URL. Provider/client/operations secrets participate in normal atomic reload. Redis/Sentinel connection settings and OpenTelemetry exporter settings remain restart-scoped because they are process infrastructure.

Do not configure both inline and file-backed versions of the same secret.

## Provider request boundary

GemGate removes inbound provider credential headers and reconstructs provider authentication from server-side configuration.

Provider HTTP redirects are **not** followed automatically. A provider `3xx` response is returned to the caller with its `Location` header. This prevents a hidden second request from carrying server-side provider/custom credentials to an unexpected redirect target, including same-origin redirect chains that may later escape to another origin.

Forwarding and tracing metadata are also rebuilt explicitly rather than blindly relayed.

## Browser boundary / CORS

CORS is browser policy, not authentication. Prefer explicit `server.cors.allowed_origins`, disable CORS for server-only deployments, and never combine wildcard `*` with `allow_credentials: true`.

## Rate limiting, Redis and Sentinel

`clients[].rate_limit_rpm` is an exact rolling one-minute window. `memory` is process-local; `redis` shares quota across replicas through an atomic Redis operation.

- prefer `rate_limit.redis.url_file` when the URL contains credentials;
- use `rediss://` or a private trusted network across network boundaries;
- raw application bearer tokens are not written into Redis keys;
- `/_config` and TUI omit Redis addresses/passwords and expose only safe mode state;
- `fail_open: false` is the default and preserves quota/spend control when Redis fails;
- `fail_open: true` is explicit opt-in and must be treated as a risk decision;
- URLs carrying `master_name` select native go-redis Sentinel failover mode.

A dedicated CI workflow forces a real Sentinel master promotion and verifies limiter-state continuity through the same failover client. Production deployments should still perform topology-specific drills for DNS, TLS, authentication, persistence and network policy.

See `docs/RATE_LIMITING.md`.

## OpenTelemetry privacy boundary

Optional OTLP/HTTP tracing is metadata-only. GemGate deliberately does **not** attach:

- URL query strings;
- request/response bodies;
- prompts or completions;
- application/operations bearer tokens;
- provider API keys;
- arbitrary headers;
- Redis URL/credentials;
- collector endpoint credentials;
- raw provider transport error strings.

Incoming `traceparent`, `tracestate`, and `baggage` are stripped before the provider trust boundary. Upstream tracing propagation is disabled by default. With `telemetry.propagate_upstream: true`, only W3C Trace Context is injected; baggage is never forwarded.

Privacy regression tests explicitly search recorded span attributes and redacted `/_config` output for sensitive fixtures.

See `docs/OBSERVABILITY.md`.

## Trusted proxies

Forwarded client IP headers are untrusted unless `server.trusted_proxies` explicitly covers the peer/proxy chain. Incoming `Forwarded`, `X-Forwarded-*` and `X-Real-IP` are removed before upstream forwarding and rebuilt from GemGate's resolved client address.

Keep trusted CIDRs narrow.

## Circuit breaker, streaming and request replay

Provider circuit breakers reduce pressure during repeated transport/HTTP 5xx failures but never replay a user generation request automatically. 4xx/429 do not trip the circuit. Threshold/open period are configurable per provider.

Provider accounting remains open until response-body EOF/Close. Truncated fixed-length responses and malformed chunked disconnects are classified as transport/circuit failures even when upstream already emitted HTTP 200. GemGate never tries to overwrite a partial response with a second HTTP error after streaming has started.

Downstream cancellation is classified separately and is not a provider failure; provider/network timeout is.

## Logging boundary

GemGate does not capture request/response bodies or arbitrary headers. Legacy `logging.log_body` and `logging.log_headers` are accepted only as `false`; `true` is rejected until a field-level redaction contract exists.

Operational logs should contain request metadata, not prompts, completions, bearer tokens or provider credentials.

## Health/readiness exposure

`/_healthz` is liveness plus passive provider health. `/_readyz` is passive readiness based on default-provider circuit state. Neither performs synthetic provider requests or consumes provider quota.

With `public_health: true`, these endpoints are intentionally unauthenticated only on the listener where operational endpoints are present. `/_metrics` and `/_config` are never public and use dedicated operations auth when configured.

## Production checklist

- Use a distinct operations token and a distinct application token per consumer.
- Run a dedicated `-operations-listen` and keep it off the public/application ingress.
- Restrict the operations listener with network policy/firewall rules.
- Prefer file-backed/orchestrator secrets.
- Terminate TLS at a trusted reverse proxy/load balancer/private ingress.
- Use Redis shared limiting for horizontally scaled replicas; use Sentinel when self-managed Redis HA needs master failover.
- Keep Redis fail-closed if quota protects material spend.
- Configure `trusted_proxies` only for infrastructure you control.
- Configure CORS explicitly or disable it.
- Restrict egress for custom provider URLs.
- Keep body/header logging disabled.
- Keep upstream trace propagation disabled unless explicit distributed-trace correlation with that provider is required.
- Use `/_healthz` for liveness and `/_readyz` for passive readiness.

## Supported versions

The project is pre-1.0. Security fixes should be applied to the latest `main` branch and latest tagged release.
