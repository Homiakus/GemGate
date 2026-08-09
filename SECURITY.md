# Security policy and deployment notes

## Reporting vulnerabilities

Please report security-sensitive issues privately to the repository owner rather than opening a public issue containing credentials, exploit details, production topology, or reproducible attack steps against a live deployment.

## Trust boundaries

GemGate has two independent credential classes:

1. client tokens (`clients[].token`) authenticate applications to GemGate;
2. provider credentials (`providers[].api_key` and server-side provider headers) authenticate GemGate to upstream AI services.

They are intentionally never interchangeable. Incoming provider-auth headers are stripped before the selected provider adapter reconstructs upstream authentication from server-side configuration.

## Browser boundary / CORS

CORS is a browser policy, not an authentication mechanism. A permissive CORS policy does not make a bearer token safe to expose in browser code.

For production:

- prefer explicit `server.cors.allowed_origins`;
- set `server.cors.enabled: false` when browsers do not call GemGate directly;
- never combine wildcard `*` with `allow_credentials: true` — GemGate rejects that configuration;
- keep client tokens scoped and independently revocable even when an origin is allowed.

For backward compatibility, configs that omit `server.cors` retain the previous wildcard-origin behavior. New deployments should configure the policy explicitly.

## Rate limiting

`clients[].rate_limit_rpm` uses an exact rolling one-minute window, preventing fixed-window boundary bursts. The limiter is still process-local: multiple GemGate replicas do not share quota state.

Do not treat the local limiter as a global billing cap in a horizontally scaled deployment. Use an external/shared enforcement layer when that property is required.

## Provider health

Provider health shown in TUI and `/_healthz` is passive telemetry derived from completed requests:

- it does not make synthetic provider requests;
- it is not a readiness guarantee;
- it is not a circuit breaker;
- it does not retry model-generation requests.

This avoids silently duplicating non-idempotent or billable generation calls.

## Production checklist

- Keep `config.yaml` mode-restricted and prefer environment variables, orchestrator secrets, or a secret manager for provider credentials.
- Put GemGate behind a TLS terminator or private network boundary; GemGate itself serves HTTP.
- Use one GemGate client token per application/user and rotate it independently of provider keys.
- Set `rate_limit_rpm` where a compromised client token could create material provider spend.
- Restrict network egress if you use custom provider URLs.
- Configure CORS explicitly or disable it for server-only deployments.
- Do not expose `/_config` or `/_metrics` without client authentication; GemGate requires it by default.
- Treat `public_health: true` as intentionally public metadata. It includes provider names and passive state, but not provider URLs or keys.
- Avoid logging request/response bodies or sensitive headers. `logging.log_body` / `logging.log_headers` are reserved for future structured logging behavior and should remain false.
- Treat custom `providers[].headers` as secrets when they contain authorization material.

## Supported versions

The project is currently pre-1.0. Security fixes should be applied to the latest `main` branch and latest tagged release.
