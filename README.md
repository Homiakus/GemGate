# GemGate

Multi-provider AI API gateway на Go: серверные provider keys, отдельные клиентские tokens, streaming passthrough, atomic hot reload, distributed rate limiting, circuit breakers, Prometheus и Charm TUI.

GemGate ставится между приложениями и AI-провайдерами. Клиент знает только свой GemGate bearer token; реальные provider credentials остаются на сервере. Gateway выбирает upstream, удаляет входные provider-auth headers, добавляет серверную авторизацию и проксирует provider-native payload/stream без искусственной трансляции схем.

GemGate **не** обходит provider quota/billing/safety rules и **не** делает скрытые retry/failover генерации.

## Что умеет

- несколько AI providers в одном процессе через `/providers/{name}/...`;
- root-route через `default_provider` для обратной совместимости;
- built-in provider auth adapters + generic OpenAI-compatible/custom endpoints;
- file-backed provider/client secrets и live rotation;
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
Client / SDK
    │ GemGate bearer token
    ▼
CORS + trusted-proxy boundary
    ▼
Immutable runtime snapshot
    ├── client auth
    ├── rate-limit policy ──► memory | Redis shared backend
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

- `internal/config` — strict YAML, defaults, secrets, CORS, trusted proxies, Redis/circuit validation;
- `internal/provider` — provider catalog и auth contract;
- `internal/gateway/runtime.go` — immutable snapshots и atomic reload;
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

Отключить polling:

```bash
gemgate serve -config config.yaml -reload-interval 0
```

## Минимальная конфигурация

```yaml
server:
  listen: ":8080"
  read_timeout: "30s"
  write_timeout: "0s"
  idle_timeout: "120s"
  public_health: true
  request_body_limit: "32MiB"
  trusted_proxies: []
  cors:
    enabled: true
    allowed_origins: ["http://localhost:3000"]
    allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type", "X-Request-ID"]
    allow_credentials: false
    max_age: "10m"

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

## Маршрутизация

Named route:

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

Клиент всегда авторизуется в GemGate:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

Этот token не пересылается provider. Provider auth создаётся заново из server-side config.

## Secrets и hot reload

Provider key:

```yaml
providers:
  - name: openai
    type: openai
    api_key_file: "/run/secrets/openai_api_key"
```

Client token:

```yaml
clients:
  - name: backend
    token_file: "/run/secrets/gemgate_backend_token"
    enabled: true
    rate_limit_rpm: 120
```

Candidate config полностью загружается, резолвит secret files, применяет defaults и проходит validation. Только после этого активный runtime заменяется целиком. Некорректная ревизия не частично применяется.

Уже начатый streaming request заканчивается на старом snapshot; новые requests после swap используют новый.

Hot-reloadable:

- providers/default provider, keys, URLs, headers, timeouts, circuit policy;
- client token/enabled/RPM;
- trusted proxies;
- CORS;
- request body limit;
- recent log-ring size.

Restart-required:

- listener/read/write/idle server settings;
- rate-limit backend и Redis connection/failure-policy settings.

## Rate limiting

### Один процесс

```yaml
rate_limit:
  backend: memory
```

`memory` — default: exact rolling 60-second window без fixed-window double burst.

### Несколько replicas

```yaml
rate_limit:
  backend: redis
  redis:
    url_file: "/run/secrets/gemgate_redis_url"
    key_prefix: "gemgate:ratelimit:"
    timeout: "2s"
    fail_open: false
```

Redis backend использует atomic Lua operation и Redis server time, поэтому две независимые GemGate replicas видят одну quota. Redis key строится из SHA-256-derived client identifier; raw bearer token в key не записывается.

По умолчанию Redis failure — **fail-closed**: GemGate отвечает локальным `503` и не вызывает AI provider. `fail_open: true` — отдельное осознанное решение, при котором запрос разрешается, а backend error логируется и увеличивает `gemgate_rate_limit_backend_errors_total`.

Redis URL/credentials не выводятся в `/_config` или TUI. Подробнее: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md).

## Circuit breaker

Каждый provider может иметь собственную policy:

```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5
  open_for: "30s"
```

- transport errors и HTTP 5xx считаются failures;
- 4xx/429 circuit не открывают;
- при threshold circuit становится `open`;
- open requests получают local `503`, `Retry-After`, `X-GemGate-Circuit: open` без upstream call;
- после cooldown допускается ровно один `half_open` probe;
- успешный probe закрывает circuit;
- **automatic retries/replay отсутствуют**.

Downstream cancellation не считается provider failure; provider/network timeout считается.

## Trusted proxies

```yaml
server:
  trusted_proxies:
    - "127.0.0.1"
    - "10.0.0.0/8"
```

GemGate доверяет forwarded IP только через явно доверенную цепочку. Spoofed `X-Forwarded-For` от обычного клиента игнорируется. Перед upstream исходные forwarding headers удаляются и формируются заново из вычисленного client IP.

Подробнее: [`docs/TRUSTED_PROXIES.md`](docs/TRUSTED_PROXIES.md).

## Operational endpoints

| Endpoint | Auth | Назначение |
| --- | --- | --- |
| `/_healthz` | public при `public_health: true`, иначе bearer | liveness + passive provider summary |
| `/_readyz` | public при `public_health: true`, иначе bearer | passive readiness default provider |
| `/_metrics` | bearer | Prometheus |
| `/_config` | bearer | redacted runtime config |

`/_readyz` не делает synthetic provider calls и не расходует quota.

Prometheus включает global/provider/circuit series и `gemgate_rate_limit_backend_errors_total`.

## TUI

Views:

1. **Overview** — traffic, latency, provider/circuit attention;
2. **Logs** — client/provider/IP-aware request log;
3. **Clients** — usage и RPM policy;
4. **Providers** — health, circuit, requests, errors, duration;
5. **Config** — redacted provider + rate-limit/CORS configuration;
6. **Help**.

Управление: `1-6`, `Tab`, `Shift+Tab`, `r`, `space/p`, `a/w/e/u`, `?`, `q`.

## CI и тестирование

Каждый push/PR проходит:

```bash
go mod verify
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

CI поднимает реальный Redis service. Integration suite проверяет shared quota между двумя limiter instances, Redis fail-open semantics, secret-safe config, SSE early flush, streaming lifetime, cancellation propagation и provider timeout classification.

Тот же CI вызывает `scripts/build-release.sh` и cross-compiles Linux/macOS/Windows для amd64/arm64, после чего запускает Linux release binary и проверяет injected version.

## Releases

Tag `vX.Y.Z` запускает `.github/workflows/release.yml`. Workflow проверяет source, собирает архивы тем же build-script, генерирует SPDX JSON SBOM и `checksums.txt`, создаёт GitHub artifact attestations и публикует GitHub Release.

Подробности и команды проверки: [`docs/RELEASING.md`](docs/RELEASING.md).

## Production checklist

- Terminate TLS на reverse proxy/load balancer/private ingress.
- Используйте отдельный GemGate token для каждого consumer.
- Предпочитайте file-backed/orchestrator secrets.
- Для multi-replica quota используйте Redis backend; держите `fail_open: false`, если quota защищает расходы.
- Используйте `rediss://` или private network для удалённого Redis.
- Настройте `trusted_proxies` только для контролируемой proxy-chain.
- Настройте CORS явно или отключите его для server-only deployment.
- Используйте `/_healthz` для liveness и `/_readyz` для passive readiness.
- Не включайте body/header logging — чувствительный capture намеренно отсутствует.
- Ограничьте egress для custom provider URLs.
- Не воспринимайте circuit breaker как retry/failover engine.

Security: [`SECURITY.md`](SECURITY.md) · Audit: [`docs/AUDIT.md`](docs/AUDIT.md) · Providers: [`docs/PROVIDERS.md`](docs/PROVIDERS.md) · Rate limiting: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md) · Releases: [`docs/RELEASING.md`](docs/RELEASING.md)

## Лицензия

См. [`LICENSE`](LICENSE).
