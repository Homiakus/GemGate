# Trusted proxies and client IP identity

GemGate does not trust `X-Forwarded-For`, `X-Real-IP` or RFC `Forwarded` headers from arbitrary clients. Client IP is derived from the TCP peer unless the request arrives through an explicitly configured trusted proxy network.

## Configuration

```yaml
server:
  trusted_proxies:
    - "127.0.0.1"
    - "10.0.0.0/8"
```

Entries may be CIDRs or individual IPv4/IPv6 addresses. An individual address is normalized to `/32` or `/128`. Invalid values reject the complete configuration revision during startup or hot reload.

The default is an empty list, which means **no proxy is trusted**.

Only configure networks that are actually controlled reverse proxies, ingress controllers or load balancers in front of GemGate. Do not add broad private ranges merely because the deployment happens to use private networking.

## Resolution algorithm

For every request GemGate first parses the direct TCP peer from `RemoteAddr`.

### Direct/untrusted peer

If the peer does not match `server.trusted_proxies`:

- the peer IP is the client IP;
- incoming `X-Forwarded-For`, `X-Real-IP`, `Forwarded`, `X-Forwarded-Host`, `X-Forwarded-Port` and `X-Forwarded-Proto` are not trusted;
- those forwarding headers are removed before the request is sent upstream.

A public client therefore cannot spoof an audit IP by sending its own `X-Forwarded-For`.

### Trusted peer

If the immediate peer is trusted, GemGate parses `X-Forwarded-For` as an IP chain and walks it **right to left** together with the direct peer.

It skips the contiguous trusted proxy suffix and selects the first untrusted address as the client identity.

Example:

```text
X-Forwarded-For: 203.0.113.7, 10.1.0.11
RemoteAddr:       10.2.0.20
trusted_proxies:  10.0.0.0/8

resolved client:  203.0.113.7
```

If an intermediate address is not trusted, traversal stops there. Values farther to the left are treated as attacker-controlled and are not selected:

```text
X-Forwarded-For: 192.0.2.99, 198.51.100.8
RemoteAddr:       10.2.0.20
trusted_proxies:  10.0.0.0/8

resolved client:  198.51.100.8
```

Malformed `X-Forwarded-For` is rejected as identity input and GemGate falls back to the trusted direct peer. `X-Real-IP` is used only as a fallback when there is no valid XFF chain and the direct peer is trusted.

## Upstream forwarding

GemGate strips all incoming forwarding metadata first. It then emits only server-generated:

```http
X-Forwarded-For: <resolved-client-ip>
X-Real-IP: <resolved-client-ip>
```

The original `Forwarded` header is not passed through. This prevents untrusted metadata from being mistaken for a server assertion by the AI provider or another upstream proxy.

## Logs and TUI

The same resolved address is stored as `LogEntry.ClientIP` and shown in the TUI Logs table/detail view. Authentication failures, rate-limit rejections, successful requests and client-cancelled requests use the same trust calculation.

## Hot reload

`server.trusted_proxies` is part of the immutable runtime snapshot and can be changed by hot reload. A candidate list is parsed and validated before the new snapshot replaces the active runtime.

As with other snapshot state, a request already in progress keeps the trust policy with which it started; new requests use the new list after the atomic swap.

## Reverse-proxy examples

### Local Caddy/Nginx on the same host

```yaml
server:
  trusted_proxies:
    - "127.0.0.1"
    - "::1"
```

### Dedicated ingress subnet

If the actual ingress addresses are in a narrow controlled subnet:

```yaml
server:
  trusted_proxies:
    - "10.42.16.0/24"
```

Prefer the narrowest network matching the real ingress deployment. The goal is not to describe every private host; it is to define exactly which peers are authorized to assert client identity.
