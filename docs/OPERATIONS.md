# Operations authentication

GemGate separates the application data plane from the operational control plane when a dedicated operations token is configured.

## Why use a separate token

Application tokens under `clients:` are intended to authorize billable/proxied AI requests. Operational endpoints expose process state, provider topology, metrics and redacted configuration. Production deployments should not give every application credential access to those surfaces.

Configure a dedicated operations token:

```yaml
operations:
  token_file: "/run/secrets/gemgate_operations_token"
```

An inline token is also supported:

```yaml
operations:
  token: "${GEMGATE_OPERATIONS_TOKEN}"
```

`token` and `token_file` are mutually exclusive. The operations token must also differ from every enabled client token; GemGate rejects a configuration that would collapse the two trust domains.

## Endpoint policy

With `operations.token` or `operations.token_file` configured:

| Endpoint | Credential |
| --- | --- |
| provider/application routes | `clients[].token` only |
| `/_metrics` | operations token |
| `/_config` | operations token |
| `/_healthz` with `public_health: false` | operations token |
| `/_readyz` with `public_health: false` | operations token |
| `/_healthz` / `/_readyz` with `public_health: true` | public by explicit configuration |

The operations token **cannot** proxy AI requests because it is not inserted into the application client-token map. Conversely, application tokens cannot access protected operational endpoints once dedicated operations auth is enabled.

## Backward compatibility

If `operations:` is omitted, GemGate preserves the historical behavior: any valid application client token can access protected operational endpoints.

This fallback exists only to keep existing deployments working. New production deployments should configure a dedicated operations token.

The TUI and redacted `/_config` expose only whether dedicated operations authentication is enabled. They never display or redact-and-display the token itself.

## Rotation

`operations.token_file` participates in the normal atomic config reload cycle. To rotate:

1. replace the secret file atomically;
2. wait for or trigger the next config reload;
3. the complete candidate runtime is validated;
4. new operational requests require the new token immediately after the atomic swap.

The old token stops working after the swap. Application requests already in flight are unaffected because the operations token is not part of provider request execution.

## Authentication details

GemGate expects:

```http
Authorization: Bearer <OPERATIONS_TOKEN>
```

Dedicated-token comparison uses constant-time byte comparison after bearer parsing. A failed operation-auth request returns `401 Unauthorized` and a `WWW-Authenticate` challenge for the `gemgate-operations` realm.

## Deployment guidance

- generate a high-entropy operations token independently from application tokens;
- prefer `token_file`/orchestrator credentials instead of committing it to YAML;
- restrict network access to `/_metrics` and `/_config` in addition to bearer authentication;
- leave `public_health: false` when health topology should not be public;
- if public liveness/readiness is required by an orchestrator, remember that `public_health: true` intentionally bypasses the operations token for only those two endpoints;
- rotate operations and application credentials independently.
