# GemGate

Multi-provider AI API gateway на Go: серверные provider keys, отдельные application/operations tokens, streaming passthrough, atomic hot reload, distributed rate limiting, circuit breakers, Prometheus и Charm TUI.

GemGate ставится между приложениями и AI-провайдерами. Приложение знает только свой GemGate bearer token; реальные provider credentials остаются на сервере. Gateway выбирает upstream, удаляет входные provider-auth headers, добавляет серверную авторизацию и проксирует provider-native payload/stream без искусственной трансляции схем.

GemGate **не** обходит provider quota/billing/safety rules и **не** делает скрытые retry/failover генерации.

## Что умеет

- несколько AI providers в одном процессе через `/providers/{name}/...`;
- root-route через `default_provider` для обратной совместимости;
- built-in provider auth adapters + generic OpenAI-compatible/custom endpoints;
- отдельный dedicated operations token для control-plane endpoints;
- file-backed provider/client/operations secrets и live rotation;
- immutable runtime snapshots и validation-before-swap hot reload;
- SSE/streaming passthrough с корректным early flush;
- downstream cancellation, отделённый от provider timeout/failure;
- exact rolling one-minute client rate limit;
- `memory` limiter для одного процесса и `redis` для общей quota нескольких replicas;
- configurable per-provider circuit breaker без automatic request replay;
- `/_healthz`, passive `/_readyz`, redacted `/_config`, Prometheus `/_metrics`;
- explicit trusted-proxy CIDR/IP model;
- configurable CORS;
- provider-aware Charm TUI;
- strict YAML (`KnownFields(true)`), race-tested CI и реальный Redis integration test;
- cross-platform release packaging, SHA-256, SPDX SBOM и GitHub artifact attestations.

## Архитектура

```text
Application client                    Operator / Prometheus
      │ client bearer                        │ operations bearer
      └──────────────┬───────────────────────┘
                     ▼
          CORS + trusted-proxy boundary
                     ▼
          Immutable runtime snapshot
            ├── client auth
            ├── operations auth
            ├── rate-limit policy ──► memory | Redis
            ├── provider router + auth adapter
            └── request/body policy
                         │
                         ▼
                   circuit breaker
                         │
                         ▼
                  metrics transport
                         │
                         ▼
                    AI provider
```

Ключевые пакеты:

- `internal/config` — strict YAML, defaults, secrets, operations auth, CORS, trusted proxies, Redis/circuit validation;
- `internal/provider` — provider catalog и auth contract;
- `internal/gateway/runtime.go` — immutable snapshots и atomic reload;
- `internal/gateway/operations_auth.go` — control-plane credential boundary;
- `internal/gateway/proxy.go` — routing/auth/streaming;
- `internal/gateway/ratelimit*.go` — memory/Redis rolling quota;
- `internal/gateway/circuitbreaker.go` — closed/open/half-open state machine;
- `internal/gateway/provider_metrics_transport.go` — stream-aware provider telemetry;
- `internal/gateway/trusted_proxy.go` — forwarded-IP trust boundary;
- `internal/tui` — operator presentation only;
- `cmd/gemgate` — CLI, watcher и process lifecycle.

Подробнее: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Provider types

| `type` | Default upstream | Auth |
| --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | Gemini native key / Bearer on OpenAI-compatible path |
| `openai` | `https://api.openai.com/v1` | Bearer |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` |
| `groq` | `https://api.groq.com/openai/v1` | Bearer |
| `mistral` | `https://api.mistral.ai/v1` | Bearer |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer |
| `deepseek` | `https://api.deepseek.com` | Bearer |
| `xai` | `https://api.x.ai/v1` | Bearer |
| `cohere` | `https://api.cohere.com/v2` | Bearer |
| `together` | `https://api.together.ai/v1` | Bearer |
| `cerebras` | `https://api.cerebras.ai/v1` | Bearer |
| `fireworks` | `https://api.fireworks.ai/inference/v1` | Bearer |
| `openai-compatible` | config | optional Bearer |
| `none` | config | no auth |

```bash
gemgate providers
```

Подробнее: [`docs/PROVIDERS.md`](docs/PROVIDERS.md).

## Быстрый старт

```bash
cp config.example.yaml config.yaml
export GEMINI_API_KEY="your-provider-key"
export GEMGATE_TOKEN="a-long-random-client-token"
go run ./cmd/gemgate run -config config.yaml
```

Headless:

```bash
go run ./cmd/gemgate serve -config config.yaml
```

Hot reload по умолчанию проверяет config/secrets каждые 5 секунд:

```bash
gemgate serve -config config.yaml -reload-interval 5s
```

## Минимальная конфигурация

```yaml
server:
  listen: ":8080"
  read_timeout: "30s"
  write_timeout: "0s"
  idle_timeout: "120s"
  public_health: false
  request_body_limit: "32MiB"
  trusted_proxies: []
  cors:
    enabled: true
    allowed_origins: ["http://localhost:3000"]
    allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type", "X-Request-ID"]
    allow_credentials: false
    max_age: "10m"

operations:
  token_file: "/run/secrets/gemgate_operations_token"

rate_limit:
  backend: memory

default_provider: gemini
providers:
  - name: gemini
    type: gemini
    api_key: "${GEMINI_API_KEY}"
    timeout: "0s"
    circuit_breaker:
      enabled: true
      failure_threshold: 5
      open_for: "30s"

clients:
  - name: local-dev
    token: "${GEMGATE_TOKEN}"
    enabled: true
    rate_limit_rpm: 120

logging:
  recent: 300
  log_body: false
  log_headers: false
```

`write_timeout: "0s"` и provider `timeout: "0s"` подходят для long-lived model streams. `logging.log_body` / `logging.log_headers` могут оставаться только `false`; `true` намеренно отклоняется.

## Application и operations auth

Application client использует:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

Этот token может проксировать AI-запросы и не пересылается provider. Provider auth создаётся заново из server-side config.

Для production рекомендуется отдельный control-plane token:

```yaml
operations:
  token_file: "/run/secrets/gemgate_operations_token"
```

После его настройки `/_metrics`, `/_config` и приватные health/readiness требуют **operations token**. Application token получает `401`, а operations token, в свою очередь, получает `401` на обычном provider route и не может инициировать billable AI request.

`operations.token`/`token_file` hot-reloadable; совпадение operations token с enabled client token отклоняется. Если `operations:` отсутствует, старые client tokens продолжают авторизовывать protected operational endpoints для backward compatibility.

Подробнее: [`docs/OPERATIONS.md`](docs/OPERATIONS.md).

## Маршрутизация

```text
/providers/{provider-name}/{provider-path}
```

Примеры:

```text
POST /providers/openai/responses
  -> https://api.openai.com/v1/responses

POST /providers/claude/v1/messages
  -> https://api.anthropic.com/v1/messages

POST /providers/together/chat/completions
  -> https://api.together.ai/v1/chat/completions
```

Пути без `/providers/...` идут в `default_provider`.

## Secrets и hot reload

Поддерживаются file-backed provider keys, application tokens и operations token. Candidate config полностью загружается, резолвит secret files, применяет defaults и проходит validation; только после этого runtime заменяется целиком.

Уже начатый streaming request заканчивается на старом snapshot; новые requests после swap используют новый.

Hot-reloadable:

- providers/default provider, keys, URLs, headers, timeouts, circuit policy;
- client token/enabled/RPM;
- operations token/token_file;
- trusted proxies;
- CORS;
- request body limit;
- recent log-ring size.

Restart-required:

- listener/read/write/idle server settings;
- rate-limit backend и Redis connection/failure-policy settings.

## Rate limiting

Single process:

```yaml
rate_limit:
  backend: memory
```

Multiple replicas:

```yaml
rate_limit:
  backend: redis
  redis:
    url_file: "/run/secrets/gemgate_redis_url"
    key_prefix: "gemgate:ratelimit:"
    timeout: "2s"
    fail_open: false
```

Redis backend использует atomic Lua operation и Redis server time, поэтому независимые GemGate replicas видят одну quota. Raw bearer token не записывается в Redis key. По умолчанию backend fail-closed: при Redis outage GemGate возвращает local `503` и не вызывает AI provider.

Redis URL/credentials не выводятся в `/_config` или TUI. Подробнее: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md).

## Circuit breaker

```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5
  open_for: "30s"
```

Transport errors и HTTP 5xx считаются failures; 4xx/429 circuit не открывают. Open requests получают local `503` без upstream call. После cooldown допускается один `half_open` probe. Automatic retries/replay отсутствуют.

Downstream cancellation не считается provider failure; provider/network timeout считается.

## Trusted proxies

```yaml
server:
  trusted_proxies:
    - "127.0.0.1"
    - "10.0.0.0/8"
```

Spoofed `X-Forwarded-For` от недоверенного peer игнорируется. Перед upstream forwarding headers очищаются и формируются заново из вычисленного client IP.

Подробнее: [`docs/TRUSTED_PROXIES.md`](docs/TRUSTED_PROXIES.md).

## Operational endpoints

| Endpoint | При dedicated operations auth | Назначение |
| --- | --- | --- |
| `/_healthz` | public при `public_health: true`, иначе operations token | liveness + passive provider summary |
| `/_readyz` | public при `public_health: true`, иначе operations token | passive readiness default provider |
| `/_metrics` | operations token | Prometheus |
| `/_config` | operations token | redacted runtime config |

`/_readyz` не делает synthetic provider calls и не расходует quota. Без configured `operations:` protected endpoints сохраняют legacy client-token fallback.

## TUI

Views:

1. **Overview** — traffic, latency, provider/circuit attention;
2. **Logs** — client/provider/IP-aware request log;
3. **Clients** — usage и RPM policy;
4. **Providers** — health, circuit, requests, errors, duration;
5. **Config** — redacted providers, rate-limit/CORS и operations-auth mode;
6. **Help**.

## CI и releases

Каждый push/PR проходит modules, gofmt, vet, `go test -race -cover ./...`, build, real Redis integration и cross-platform release packaging smoke.

Tag `vX.Y.Z` собирает Linux/macOS/Windows для amd64/arm64, генерирует SHA-256 checksums и SPDX SBOM, создаёт GitHub artifact attestations и публикует GitHub Release.

Подробнее: [`docs/RELEASING.md`](docs/RELEASING.md).

## Production checklist

- Используйте отдельный operations token и отдельный application token для каждого consumer.
- Предпочитайте file-backed/orchestrator secrets.
- Terminate TLS на trusted reverse proxy/load balancer/private ingress.
- Для multi-replica quota используйте Redis backend и fail-closed policy, если quota защищает расходы.
- Используйте `rediss://` или private network для удалённого Redis.
- Настройте `trusted_proxies` только для контролируемой proxy-chain.
- Настройте CORS явно или отключите его для server-only deployment.
- Не включайте body/header logging.
- Ограничьте egress для custom provider URLs.
- Не воспринимайте circuit breaker как retry/failover engine.

Security: [`SECURITY.md`](SECURITY.md) · Audit: [`docs/AUDIT.md`](docs/AUDIT.md) · Operations: [`docs/OPERATIONS.md`](docs/OPERATIONS.md) · Providers: [`docs/PROVIDERS.md`](docs/PROVIDERS.md) · Rate limiting: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md) · Releases: [`docs/RELEASING.md`](docs/RELEASING.md)

## Лицензия

См. [`LICENSE`](LICENSE).
