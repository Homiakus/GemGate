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

## Redis Sentinel failover

GemGate automatically switches the Redis client to go-redis Sentinel failover mode when the configured URL contains a non-empty `master_name` query parameter.

Example secret file content:

```text
redis://sentinel-1.internal:26379/0?master_name=gemgate-master&addr=sentinel-2.internal%3A26379&addr=sentinel-3.internal%3A26379
```

No extra `rate_limit.backend` value is required: it remains `redis`. The same rolling-window Lua script runs against the master selected by Sentinel. When Sentinel promotes a new master, the go-redis failover client resolves the new master instead of pinning GemGate to one Redis node.

Use at least three Sentinel processes in a production quorum topology. GemGate seed URLs may include multiple `addr=` values so the client is not dependent on one Sentinel seed.

The failover URL follows the native go-redis `ParseFailoverURL` contract. If Sentinel and Redis data nodes require authentication, keep the full URL in `url_file`; do not place those credentials in repository YAML. TLS Sentinel deployments should use `rediss://` and a certificate-valid hostname.

GemGate deliberately does not implement its own leader election or Redis failover state machine. Sentinel ownership stays in Redis/go-redis; GemGate only consumes the active-master abstraction.

The repository has a dedicated `Redis Sentinel E2E` workflow. It starts a Redis master, replica and three Sentinel processes, writes limiter state, forces `SENTINEL FAILOVER`, waits for a different master, then verifies that the **same** GemGate failover client reconnects and sees the pre-promotion quota state on the promoted replica.

## Managed Redis

For a managed Redis service that exposes one stable primary endpoint, use the normal standalone URL supplied by the service. Provider-side replication/failover remains transparent to GemGate.

Use Sentinel mode only when the service actually exposes Redis Sentinel semantics. Do not add `master_name` merely because the managed service is highly available.

For cluster-sharded Redis, verify that the deployment supports the Lua + sorted-set workload and keeps each GemGate limiter key on a single authoritative shard. GemGate currently uses a single-node/failover `redis.Client` abstraction rather than Redis Cluster routing; native Redis Cluster support should be treated as a separate feature, not assumed from Sentinel support.

## Redis credentials

Redis URLs may contain usernames/passwords. Prefer `url_file` over committing a credential-bearing URL to `config.yaml`:

```text
rediss://user:password@redis.internal.example:6379/0
```

`/_config` and the TUI expose only the backend name, `standalone`/`sentinel` mode, whether Redis is configured, key prefix, timeout and fail-open policy. The Redis URL and credentials are intentionally omitted.

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
- Redis/Sentinel URL / URL file;
- Redis key prefix;
- Redis timeout;
- Redis fail-open policy.

The backend is process infrastructure rather than request-snapshot state. Requiring restart avoids partially replacing a shared Redis client while requests are in flight.

## Failure drills

CI now covers a real forced Sentinel promotion, but production topology still deserves failure drills because network policy, TLS, DNS, authentication and persistence differ from the test environment:

1. stop the active Redis primary and verify Sentinel/managed failover restores limiter requests;
2. isolate one Sentinel seed and verify another seed can resolve the master;
3. make Redis entirely unreachable and verify `fail_open: false` produces local `503` with zero AI-provider calls;
4. repeat with `fail_open: true` only if that is the explicit production policy and verify the backend-error metric/alerts;
5. rotate Redis credentials and confirm the planned process restart succeeds before retiring the old credentials;
6. verify NTP/host clock changes do not affect quota semantics because Redis `TIME`, not GemGate wall clock, owns the shared window.

## Testing

The normal CI starts a real standalone Redis service and verifies under `go test -race` that:

1. two independent limiter instances see one shared quota for the same token;
2. different tokens remain isolated;
3. fail-open behavior is explicit;
4. raw bearer tokens never appear in limiter keys;
5. Redis connection credentials are absent from the redacted config surface;
6. normal URLs select standalone Redis mode;
7. failover URLs with `master_name` select go-redis Sentinel mode and malformed failover options are rejected before serving traffic.

The separate Sentinel workflow additionally verifies quorum discovery, forced master promotion, reconnection through the existing failover client and continuity of limiter state across promotion.
