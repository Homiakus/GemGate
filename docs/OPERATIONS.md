# Operations authentication and listener isolation

GemGate supports two independent layers for the operational control plane:

1. a dedicated operations bearer token separates credentials from application/client tokens;
2. an optional dedicated HTTP listener removes operational routes from the application port entirely.

They can be used independently, but production deployments should normally use both.

## Dedicated operations token

Application tokens under `clients:` authorize billable/proxied AI requests. Operational endpoints expose process state, provider topology, metrics and redacted configuration. Production deployments should not give every application credential access to those surfaces.

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

`token` and `token_file` are mutually exclusive. The operations token must differ from every enabled client token; GemGate rejects a configuration that would collapse the two trust domains.

## Dedicated operations listener

Start GemGate with a second listener:

```bash
gemgate serve \
  -config config.yaml \
  -operations-listen 127.0.0.1:9090
```

When `-operations-listen` is set, handler composition and listener binding complete **before** either server starts accepting requests. There is no startup interval where the application listener still exposes operational routes.

The routing contract becomes:

| Listener | Application/provider paths | `/_healthz` `/_readyz` `/_metrics` `/_config` |
| --- | --- | --- |
| application `server.listen` | normal GemGate proxy | `404 Not Found` |
| `-operations-listen` | `404 Not Found` | handled locally |

The operations listener never contains an application proxy path, so even a valid application token cannot accidentally turn that port into an AI egress path. Conversely, operational endpoints are not reachable on the public/application listener once isolation is enabled.

The listen address is process-scoped. Changing it requires a process restart because it is a CLI deployment boundary, not hot-reloadable request state.

Recommended deployment patterns:

- bind to `127.0.0.1:9090` when Prometheus/orchestration health checks run on the same host;
- bind to a private pod/container interface when a cluster-side monitoring network needs access;
- expose only the application listener through public ingress/load balancers;
- apply firewall/network-policy rules to the operations listener even when a dedicated token is configured.

Do not reuse the same address as `server.listen`; GemGate rejects that startup configuration.

## Endpoint credential policy

With `operations.token` or `operations.token_file` configured:

| Endpoint | Credential |
| --- | --- |
| provider/application routes | `clients[].token` only |
| `/_metrics` | operations token |
| `/_config` | operations token |
| `/_healthz` with `public_health: false` | operations token |
| `/_readyz` with `public_health: false` | operations token |
| `/_healthz` / `/_readyz` with `public_health: true` | public by explicit configuration, but only on the operations listener when listener isolation is enabled |

The operations token **cannot** proxy AI requests because it is not inserted into the application client-token map. Conversely, application tokens cannot access protected operational endpoints once dedicated operations auth is enabled.

## Backward compatibility

If `operations:` is omitted, GemGate preserves historical credential behavior: any valid application client token can access protected operational endpoints.

If `-operations-listen` is omitted, operational endpoints remain on the application listener as before.

These fallbacks keep existing deployments working. New production deployments should normally configure both a dedicated operations token and an isolated operations listener.

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

## Security properties tested in CI

Integration tests verify that:

- application requests continue to proxy normally on the application handler;
- operational paths return 404 on the application handler after isolation is enabled;
- application/provider paths return 404 on the operations handler;
- an operations token can access protected operational endpoints;
- public health/readiness, when explicitly enabled, remain public only on the operations handler after isolation;
- the operations handler never reaches an AI provider for an application path.

## Deployment checklist

- generate a high-entropy operations token independently from application tokens;
- prefer `token_file`/orchestrator credentials instead of committing it to YAML;
- use `-operations-listen` to obtain a real network-level control-plane boundary;
- do not publish the operations listener through the application ingress;
- restrict the operations listener by firewall/security group/Kubernetes NetworkPolicy;
- leave `public_health: false` when health topology should not be unauthenticated;
- if public liveness/readiness is required by an orchestrator, scope network exposure to the operations listener;
- rotate operations and application credentials independently.
