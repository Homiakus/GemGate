# Security policy and deployment notes

## Reporting vulnerabilities

Please report security-sensitive issues privately to the repository owner rather than opening a public issue containing credentials, exploit details, production topology, or reproducible attack steps against a live deployment.

## Trust boundaries

GemGate has two independent credential classes:

1. client tokens (`clients[].token` / `clients[].token_file`) authenticate applications to GemGate;
2. provider credentials (`providers[].api_key` / `providers[].api_key_file` plus server-side provider headers) authenticate GemGate to upstream AI services.

They are intentionally never interchangeable. Incoming provider-auth headers are stripped before the selected provider adapter reconstructs upstream authentication from server-side configuration.

## Secret delivery and rotation

For production, prefer environment/orchestrator credentials or file-backed secrets over embedding credentials directly in `config.yaml`.

File-backed secrets are useful with Docker/Kubernetes/systemd credential mounts:

```yaml
providers:
  - name: openai
    type: openai
    api_key_file: "/run/secrets/openai_api_key"

clients:
  - name: backend
    token_file: "/run/secrets/gemgate_backend_token"
    enabled: true
```

GemGate re-reads referenced secret files on the configured reload interval. A candidate runtime is fully loaded and validated before it replaces the active one. Invalid/empty secret files or invalid config revisions are rejected as a whole; already active credentials remain in use until a valid revision is available.

For rotation, prefer atomic file replacement provided by the secret manager/runtime. Existing in-flight requests retain the runtime snapshot they started with; new requests receive the new credential only after a successful atomic swap.

Do not configure both inline and file-backed versions of the same secret.

## Browser boundary / CORS

CORS is browser policy, not authentication. A permissive CORS policy does not make a bearer token safe to expose in browser code.

For production:

- prefer explicit `server.cors.allowed_origins`;
- set `server.cors.enabled: false` when browsers do not call GemGate directly;
- never combine wildcard `*` with `allow_credentials: true` — GemGate rejects that configuration;
- keep client tokens scoped and independently revocable even when an origin is allowed.

For backward compatibility, configs omitting `server.cors` retain wildcard-origin behavior. New deployments should configure the policy explicitly.

## Rate limiting

`clients[].rate_limit_rpm` uses an exact rolling one-minute window, preventing fixed-window boundary bursts. The limiter is still process-local: multiple GemGate replicas do not share quota state.

Do not treat the local limiter as a global billing cap in a horizontally scaled deployment. Use an external/shared enforcement layer when that property is required.

## Circuit breaker and request replay

GemGate uses a provider circuit breaker for repeated transport/HTTP 5xx failures. The breaker reduces pressure on a provider that is clearly failing, but it is deliberately **not** a retry mechanism.

Current behavior:

- 4xx/429 do not open the circuit;
- 5 consecutive transport/5xx failures open it;
- while open, GemGate returns a local 503 and does not contact upstream;
- after 30 seconds one half-open request is admitted as a recovery probe;
- GemGate never automatically replays a user generation request.

This is an important billing/idempotency boundary: a client must explicitly decide whether and how to retry a failed request.

## Health/readiness exposure

`/_healthz` is liveness plus passive provider health. `/_readyz` is passive readiness based on default-provider circuit state. Neither endpoint performs synthetic provider requests.

When `server.public_health: true`, both are intentionally public metadata endpoints. They expose provider names/state but not provider URLs or credentials. When `public_health: false`, both remain local GemGate endpoints protected by normal client bearer authentication; they are never forwarded upstream.

`/_metrics` and `/_config` always require a GemGate bearer token.

## Production checklist

- Keep `config.yaml` mode-restricted and prefer environment variables, orchestrator credentials, or file-backed secrets.
- Put GemGate behind a TLS terminator or private network boundary; GemGate itself serves HTTP.
- Use one GemGate client token per application/user and rotate it independently of provider keys.
- Set `rate_limit_rpm` where a compromised client token could create material provider spend.
- Restrict network egress if you use custom provider URLs.
- Configure CORS explicitly or disable it for server-only deployments.
- Use `/_healthz` for liveness and `/_readyz` for passive readiness; do not convert readiness into hidden billable probes.
- Avoid logging request/response bodies or sensitive headers. `logging.log_body` / `logging.log_headers` are reserved for future structured logging behavior and should remain false.
- Treat custom `providers[].headers` as secrets when they contain authorization material.
- Do not trust forwarded client IP headers until an explicit trusted-proxy policy is configured; GemGate currently does not derive security decisions from them.

## Supported versions

The project is pre-1.0. Security fixes should be applied to the latest `main` branch and latest tagged release.
