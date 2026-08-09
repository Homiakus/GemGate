# Security policy and deployment notes

## Reporting vulnerabilities

Please report security-sensitive issues privately to the repository owner rather than opening a public issue containing credentials, exploit details, production topology, or reproducible attack steps against a live deployment.

## Trust boundaries

GemGate supports three independent credential classes:

1. application tokens (`clients[].token` / `clients[].token_file`) authorize proxied AI requests;
2. operations token (`operations.token` / `operations.token_file`) isolates `/_config`, `/_metrics` and private health/readiness from application credentials;
3. provider credentials (`providers[].api_key` / `providers[].api_key_file` plus server-side provider headers) authenticate GemGate to upstream AI services.

A configured operations token must differ from every enabled application token. It is stored outside the client-token map and cannot proxy provider requests. Conversely, application tokens cannot access protected operational endpoints once dedicated operations auth is enabled.

Incoming provider-auth headers are stripped before the selected provider adapter reconstructs upstream authentication from server-side configuration.

## Operations control plane

Production deployments should configure:

```yaml
operations:
  token_file: "/run/secrets/gemgate_operations_token"
```

When configured, this token is required for `/_metrics`, `/_config`, and for `/_healthz` / `/_readyz` when `server.public_health: false`. `public_health: true` intentionally keeps only health/readiness public.

If `operations:` is absent, application tokens retain access to protected operational endpoints for backward compatibility. Treat that as a legacy compatibility mode rather than the preferred production posture.

The operations secret is hot-reloadable and never displayed in TUI or `/_config`. See `docs/OPERATIONS.md`.

## Secret delivery and rotation

Prefer environment/orchestrator credentials or file-backed secrets over embedding credentials directly in `config.yaml`.

Supported file-backed secrets include provider keys, application tokens, operations token and Redis URL. Provider/client/operations secrets participate in normal atomic reload. Redis connection settings remain restart-scoped because the Redis client is process infrastructure.

Do not configure both inline and file-backed versions of the same secret.

## Browser boundary / CORS

CORS is browser policy, not authentication. Prefer explicit `server.cors.allowed_origins`, disable CORS for server-only deployments, and never combine wildcard `*` with `allow_credentials: true`.

## Rate limiting and Redis

`clients[].rate_limit_rpm` is an exact rolling one-minute window. `memory` is process-local; `redis` shares quota across replicas through an atomic Redis operation.

- prefer `rate_limit.redis.url_file` when the URL contains credentials;
- use `rediss://` or a private trusted network across network boundaries;
- raw application bearer tokens are not written into Redis keys;
- `/_config` and TUI omit the Redis URL/password;
- `fail_open: false` is the default and preserves quota/spend control when Redis fails;
- `fail_open: true` is explicit opt-in and must be treated as a risk decision.

See `docs/RATE_LIMITING.md`.

## Trusted proxies

Forwarded client IP headers are untrusted unless `server.trusted_proxies` explicitly covers the peer/proxy chain. Incoming `Forwarded`, `X-Forwarded-*` and `X-Real-IP` are removed before upstream forwarding and rebuilt from GemGate's resolved client address.

Keep trusted CIDRs narrow.

## Circuit breaker and request replay

Provider circuit breakers reduce pressure during repeated transport/HTTP 5xx failures but never replay a user generation request automatically. 4xx/429 do not trip the circuit. Threshold/open period are configurable per provider.

Downstream cancellation is not classified as provider failure; provider/network timeout is.

## Logging boundary

GemGate does not capture request/response bodies or arbitrary headers. Legacy `logging.log_body` and `logging.log_headers` are accepted only as `false`; `true` is rejected until a field-level redaction contract exists.

Operational logs should contain request metadata, not prompts, completions, bearer tokens or provider credentials.

## Health/readiness exposure

`/_healthz` is liveness plus passive provider health. `/_readyz` is passive readiness based on default-provider circuit state. Neither performs synthetic provider requests.

With `public_health: true`, these two endpoints are intentionally public. Otherwise they follow operations authentication. `/_metrics` and `/_config` are never public and use dedicated operations auth when configured.

## Production checklist

- Use a distinct operations token and a distinct application token per consumer.
- Prefer file-backed/orchestrator secrets.
- Terminate TLS at a trusted reverse proxy/load balancer/private ingress.
- Use Redis shared limiting for horizontally scaled replicas that need one quota.
- Keep Redis fail-closed if quota protects material spend.
- Configure `trusted_proxies` only for infrastructure you control.
- Configure CORS explicitly or disable it.
- Restrict egress for custom provider URLs.
- Keep body/header logging disabled.
- Use `/_healthz` for liveness and `/_readyz` for passive readiness.

## Supported versions

The project is pre-1.0. Security fixes should be applied to the latest `main` branch and latest tagged release.
