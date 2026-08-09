# Security policy and deployment notes

## Reporting vulnerabilities

Please report security-sensitive issues privately to the repository owner rather than opening a public issue containing credentials, exploit details, or production topology.

## Trust boundaries

GemGate has two independent credential classes:

1. client tokens (`clients[].token`) authenticate applications to GemGate;
2. provider API keys (`providers[].api_key`) authenticate GemGate to upstream AI services.

They are intentionally never interchangeable. Incoming provider-auth headers are stripped and reconstructed from server-side configuration.

## Production checklist

- Keep `config.yaml` mode-restricted and prefer environment variables or a secret manager for API keys.
- Put GemGate behind a TLS terminator or private network boundary; GemGate itself serves HTTP.
- Use one GemGate client token per application/user and rotate it independently of provider keys.
- Set `rate_limit_rpm` where a compromised client token could create material provider spend.
- Restrict network egress if you use custom provider URLs.
- Do not expose `/_config` or `/_metrics` without client authentication; GemGate requires it by default.
- Treat `public_health: true` as intentionally public metadata.
- Avoid `logging.log_body` / `logging.log_headers` for sensitive workloads. These options are reserved for future structured logging behavior and should remain false.

## Supported versions

The project is currently pre-1.0. Security fixes should be applied to the latest `main` branch and latest tagged release.
