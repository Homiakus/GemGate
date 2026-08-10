# GemGate

Multi-provider AI API gateway на Go: серверные provider keys, отдельные application/operations tokens, streaming passthrough, atomic hot reload, Redis/Sentinel rate limiting, circuit breakers, Prometheus, OpenTelemetry и Charm TUI.

GemGate ставится между приложениями и AI-провайдерами. Приложение знает только свой GemGate bearer token; реальные provider credentials остаются на сервере. Gateway выбирает upstream, очищает входную авторизацию, добавляет серверный provider auth и проксирует provider-native payload/stream без искусственной трансляции схем.

GemGate **не** обходит provider quota/billing/safety rules, **не** делает скрытые retry/failover генерации и **не** пишет prompts/completions в logs/traces.

## Что умеет

- несколько AI providers в одном процессе через `/providers/{name}/...`;
- root-route через `default_provider` для обратной совместимости;
- built-in provider auth adapters + generic OpenAI-compatible/custom endpoints;
- dedicated operations token и опциональный отдельный operations listener;
- file-backed provider/client/operations secrets и atomic live rotation;
- immutable runtime snapshots и validation-before-swap hot reload;
- SSE/streaming passthrough с early flush и корректным учётом truncated streams;
- downstream cancellation, отделённый от provider timeout/failure;
- exact rolling one-minute client rate limit;
- `memory`, shared Redis и Redis Sentinel failover;
- configurable per-provider circuit breaker без automatic request replay;
- `/_healthz`, passive `/_readyz`, redacted `/_config`, Prometheus `/_metrics`;
- metadata-only OpenTelemetry OTLP/HTTP tracing с privacy regression tests;
- explicit trusted-proxy CIDR/IP model и configurable CORS;
- responsive keyboard-first Charm TUI;
- strict YAML, race-tested CI, real Redis integration и forced Sentinel promotion E2E;
- cross-platform release packaging, SHA-256, SPDX SBOM и GitHub artifact attestations.

## Быстрый старт

```bash
cp config.example.yaml config.yaml
export GEMINI_API_KEY="your-provider-key"
export GEMGATE_TOKEN="a-long-random-client-token"
go run ./cmd/gemgate serve -config config.yaml
```

Для production control-plane isolation:

```bash
gemgate serve \
  -config config.yaml \
  -operations-listen 127.0.0.1:9090
```

При `-operations-listen` маршруты `/_healthz`, `/_readyz`, `/_metrics`, `/_config` возвращают `404` на application-порту и существуют только на отдельном operations listener. Application/provider routes на operations listener также всегда возвращают `404`.

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

telemetry:
  enabled: false
  service_name: "gemgate"
  sample_ratio: 0.10
  propagate_upstream: false

logging:
  recent: 300
  log_body: false
  log_headers: false
```

`write_timeout: "0s"` и provider `timeout: "0s"` подходят для long-lived model streams. Sensitive body/header capture намеренно отсутствует; `logging.log_body: true` и `logging.log_headers: true` отклоняются валидатором.

## Application и operations plane

Application client использует:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

Этот token не пересылается provider. Provider auth создаётся заново из server-side config.

Для production рекомендуется отдельный control-plane token:

```yaml
operations:
  token_file: "/run/secrets/gemgate_operations_token"
```

И отдельный listener:

```bash
gemgate serve -config config.yaml -operations-listen 127.0.0.1:9090
```

Так separation работает сразу на двух уровнях: credentials и network surface. `operations.token`/`token_file` hot-reloadable; listen address задаётся при старте процесса. Подробнее: [`docs/OPERATIONS.md`](docs/OPERATIONS.md).

## Маршрутизация

```text
/providers/{provider-name}/{provider-path}
```

Примеры:

```text
POST /providers/openai/responses
  -> https://api.openai.com/v1/responses

POST /providers/anthropic/v1/messages
  -> https://api.anthropic.com/v1/messages

POST /providers/together/chat/completions
  -> https://api.together.ai/v1/chat/completions
```

Пути без `/providers/...` идут в `default_provider`.

Provider redirects автоматически **не followятся**: `3xx + Location` возвращается вызывающему клиенту. Это не позволяет скрытому redirect-запросу унести server-side provider credentials на другой URL.

## Secrets и hot reload

Поддерживаются file-backed provider keys, application tokens и operations token. Candidate config полностью загружается, резолвит secrets, применяет defaults и проходит validation; только после этого runtime заменяется целиком.

Hot-reloadable:

- providers/default provider, keys, URLs, headers, timeouts, circuit policy;
- client token/enabled/RPM;
- operations token/token_file;
- trusted proxies, CORS, request body limit, recent log-ring size.

Restart/process-scoped:

- application/operations listeners;
- server read/write/idle settings;
- rate-limit backend и Redis/Sentinel connection/failure-policy settings;
- OpenTelemetry exporter/sampler/propagation settings.

## Redis и Sentinel rate limiting

Single process:

```yaml
rate_limit:
  backend: memory
```

Shared Redis:

```yaml
rate_limit:
  backend: redis
  redis:
    url_file: "/run/secrets/gemgate_redis_url"
    key_prefix: "gemgate:ratelimit:"
    timeout: "2s"
    fail_open: false
```

Sentinel failover выбирается через secret URL с `master_name`:

```text
redis://sentinel-1:26379/0?master_name=gemgate-master&addr=sentinel-2%3A26379&addr=sentinel-3%3A26379
```

Redis backend использует atomic Lua operation и Redis server time. Raw bearer token не записывается в Redis key. По умолчанию backend fail-closed: при outage GemGate возвращает local `503` и не вызывает AI provider.

Отдельный CI workflow реально поднимает master + replica + 3 Sentinel, принудительно выполняет `SENTINEL FAILOVER` и проверяет, что тот же limiter client переподключается и видит quota state после promotion.

Подробнее: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md).

## OpenTelemetry

Optional OTLP/HTTP tracing:

```yaml
telemetry:
  enabled: true
  service_name: "gemgate"
  endpoint: "http://otel-collector:4318/v1/traces"
  sample_ratio: 0.10
  environment: "production"
  propagate_upstream: false
```

GemGate создаёт server span для входного request и client span для provider request. Provider span живёт до EOF/Close response body, поэтому отражает полный streaming lifetime.

В traces **не попадают** query string, request/response body, prompts, completions, bearer tokens, provider keys, arbitrary headers или collector credentials. Входные `traceparent`/`tracestate`/`baggage` очищаются на provider boundary. При явном `propagate_upstream: true` отправляется только W3C Trace Context; baggage никогда не forwardится.

Подробнее: [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md).

## Circuit breaker и streaming

Transport errors и HTTP 5xx считаются provider failures; 4xx/429 circuit не открывают. Open circuit возвращает local `503`, после cooldown допускается один `half_open` probe. Automatic retries/replay отсутствуют.

Streaming accounting остаётся открытым до EOF/Close. Truncated fixed-length и malformed chunked responses считаются transport failures даже если upstream уже успел отправить `200`; уже отправленный partial response не переписывается вторым HTTP error. Downstream cancellation учитывается отдельно.

## Operational endpoints

| Endpoint | Назначение |
| --- | --- |
| `/_healthz` | liveness + passive provider summary |
| `/_readyz` | passive readiness default provider |
| `/_metrics` | Prometheus |
| `/_config` | redacted runtime config |

С dedicated operations token protected endpoints требуют operations credential. С `-operations-listen` эти endpoints физически отсутствуют на application port.

`/_config` и TUI показывают только безопасные состояния: Redis `standalone|sentinel`, telemetry enabled/sample/propagation/endpoint-configured. Redis URL, collector endpoint и credentials не выводятся.

## TUI

TUI построен как terminal-first operator workspace, а не как набор декоративных dashboard-карточек. Основные разделы:

1. **Overview** — компактное текущее состояние: traffic, success/error, p95, in-flight, rate-limit events, provider attention и последний request;
2. **Requests** — selectable request table, contextual filters и detail выбранного запроса;
3. **Providers** — selectable master-detail view с passive health, circuit, error rate, latency и route/config context;
4. **Clients** — selectable usage/RPM table с detail выбранного consumer;
5. **Config** — scrollable redacted runtime/security view.

`?` открывает контекстную справку поверх текущего раздела; отдельного Help-screen нет. `1-5` мгновенно переключают разделы, `Tab`/`]` и `Shift+Tab`/`[` переходят вперёд/назад, `Esc` возвращает в Overview, `q` завершает TUI. Request filters `a/w/e/u` активны только в Requests и не перехватывают ввод в остальных разделах.

Responsive layout:

- `>=118` колонок — navigation rail + workspace;
- `80-117` — компактная horizontal section navigation + workspace;
- `54-79` — single-pane compact mode с текущим разделом;
- меньше `54×14` — явное состояние `Terminal too small`, без отрицательных widths/сломанных рамок.

Requests/Providers/Clients скрывают вторичные колонки по breakpoint вместо горизонтального хаоса. Config и Help используют scrollable viewport. Terminal-width tests проверяют wide/medium/compact/minimum/tiny layouts, а Unicode tests проверяют display-width обрезку кириллицы и CJK.

## CI и releases

Каждый push/PR проходит:

```text
go mod verify
gofmt
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
release packaging smoke
```

Дополнительно CI использует реальный Redis и отдельный Sentinel promotion workflow. Tag `vX.Y.Z` собирает Linux/macOS/Windows для amd64/arm64, генерирует SHA-256 checksums и SPDX SBOM, создаёт GitHub artifact attestations и публикует release assets.

## Production checklist

- Используйте отдельный application token на consumer и отдельный operations token.
- Запускайте отдельный `-operations-listen` и не публикуйте его через application ingress.
- Ограничьте operations port firewall/security-group/NetworkPolicy.
- Предпочитайте file-backed/orchestrator secrets.
- Terminate TLS на trusted reverse proxy/load balancer/private ingress.
- Для multi-replica quota используйте Redis; при самостоятельном Redis HA используйте Sentinel и fail-closed policy, если quota защищает расходы.
- Используйте `rediss://` или private network для удалённого Redis.
- Настройте `trusted_proxies` только для контролируемой proxy-chain.
- Настройте CORS явно или отключите его для server-only deployment.
- Не включайте body/header logging.
- Ограничьте egress для custom provider URLs.
- Не воспринимайте circuit breaker как retry/failover engine.

Security: [`SECURITY.md`](SECURITY.md) · Audit: [`docs/AUDIT.md`](docs/AUDIT.md) · Operations: [`docs/OPERATIONS.md`](docs/OPERATIONS.md) · Observability: [`docs/OBSERVABILITY.md`](docs/OBSERVABILITY.md) · Providers: [`docs/PROVIDERS.md`](docs/PROVIDERS.md) · Rate limiting: [`docs/RATE_LIMITING.md`](docs/RATE_LIMITING.md) · Releases: [`docs/RELEASING.md`](docs/RELEASING.md)

## Лицензия

См. [`LICENSE`](LICENSE).
