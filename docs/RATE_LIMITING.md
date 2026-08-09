# Rate limiting

GemGate applies `clients[].rate_limit_rpm` before a request reaches an AI provider. The semantic contract is an exact rolling one-minute window per client token.

## Backends

### Memory

```yaml
rate_limit:
  backend: memory
```

`memory` is the default. It is dependency-free and preserves limiter state across ordinary GemGate config reloads, but quota is local to one process. Use it for a single GemGate instance or when an external layer already enforces global quota.

### Redis

```yaml
rate_limit:
  backend: redis
  redis:
    url_file: "/run/secrets/gemgate_redis_url"
    key_prefix: "gemgate:ratelimit:"
    timeout: "2s"
    fail_open: false
```

The Redis backend is intended for multiple GemGate replicas that must share one client quota.

Properties:

- one atomic Lua operation removes expired events, counts the current window and admits/rejects the new event;
- Redis `TIME` is the clock source, so replica clock skew does not create different quota windows;
- each admitted request receives a collision-resistant member id composed from a random process id and monotonic sequence;
- Redis keys contain a truncated SHA-256 identifier derived from the GemGate bearer token, never the bearer token itself;
- entries expire automatically after the rolling window and do not require explicit token cleanup;
- a rejected request receives `429` and `Retry-After` computed from the oldest event still inside the shared window.

## Redis credentials

Redis URLs may contain usernames/passwords. Prefer `url_file` over committing a credential-bearing URL to `config.yaml`:

```text
rediss://user:password@redis.internal.example:6379/0
```

`/_config` and the TUI expose only the backend name, whether Redis is configured, key prefix, timeout and fail-open policy. The Redis URL and credentials are intentionally omitted.

Use `rediss://` or a private trusted network when Redis traffic crosses a host/network boundary.

## Failure policy

Default production behavior is fail-closed:

```yaml
fail_open: false
```

If Redis is unavailable, a rate-limited client receives local `503 Service Unavailable`; GemGate does not contact the AI provider. This prevents a Redis outage from silently removing a spending/control boundary.

`fail_open: true` is available for deployments where request availability is more important than quota enforcement. A Redis error then allows the request, writes a warning log entry and increments:

```text
gemgate_rate_limit_backend_errors_total
```

Treat fail-open as an explicit risk decision, not a normal production default.

## Reload semantics

Hot-reloadable:

- client token / token file;
- client enabled state;
- `clients[].rate_limit_rpm`.

Restart-required:

- `rate_limit.backend`;
- Redis URL / URL file;
- Redis key prefix;
- Redis timeout;
- Redis fail-open policy.

The backend is process infrastructure rather than request-snapshot state. Requiring restart avoids partially replacing a shared Redis client while requests are in flight.

## Testing

Repository CI starts a real Redis service and verifies under `go test -race` that:

1. two independent limiter instances see one shared quota for the same token;
2. different tokens remain isolated;
3. fail-open behavior is explicit;
4. raw bearer tokens never appear in limiter keys;
5. Redis connection credentials are absent from the redacted config surface.
