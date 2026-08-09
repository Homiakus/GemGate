# GemGate

<p align="center">
  <strong>Multi-provider AI API gateway на Go: серверные ключи, клиентские токены, hot reload, streaming, resilience, observability и Charm TUI.</strong>
</p>

<p align="center">
  <img alt="Go 1.25+" src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white">
  <img alt="Charm TUI" src="https://img.shields.io/badge/TUI-Charm-6d28d9">
  <img alt="Docker ready" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white">
  <img alt="Prometheus metrics" src="https://img.shields.io/badge/Metrics-Prometheus-E6522C?logo=prometheus&logoColor=white">
</p>

GemGate ставится между приложениями и AI-провайдерами. Клиент знает только собственный GemGate bearer token; реальные API keys OpenAI, Gemini, Anthropic и других сервисов остаются на сервере. Gateway выбирает upstream по маршруту, удаляет входные provider credentials, добавляет серверную авторизацию и прозрачно проксирует provider-native payloads и streaming responses.

GemGate **не** пытается превращать разные API в один искусственный формат. OpenAI-compatible endpoints остаются OpenAI-compatible, Anthropic остаётся Anthropic, Gemini native остаётся Gemini native. Это уменьшает поверхность ошибок и позволяет использовать новые возможности провайдеров без ожидания обновления транслятора.

> GemGate не обходит квоты, billing, safety policies или upstream rate limits и не делает скрытых retry генерации.

## Возможности

| Возможность | Что даёт |
| --- | --- |
| Multi-provider routing | Несколько AI upstream в одном процессе через `/providers/{name}/...`. |
| Default provider | Root URLs продолжают работать через `default_provider`. |
| Server-side credentials | Provider API keys не выдаются приложениям. |
| File-backed secrets | `api_key_file` и `token_file` подходят для Docker/Kubernetes/systemd credentials и live rotation. |
| Atomic hot reload | Новый config полностью валидируется и только потом заменяет текущий runtime snapshot. |
| In-flight safety | Уже начатый streaming request заканчивается на старом snapshot; новые запросы сразу используют новый. |
| Credential isolation | Клиентские provider-auth headers не проходят upstream. |
| Provider auth adapters | Bearer, Gemini native/OpenAI mode, Anthropic `x-api-key`, no-auth/custom endpoints. |
| Streaming passthrough | SSE и другие streaming responses идут по мере поступления. |
| Sliding-window rate limit | Точное rolling one-minute окно без fixed-window double burst. |
| Circuit breaker | После серии transport/5xx failures проблемный provider временно отсоединяется без retry запросов. |
| Passive readiness | `/_readyz` учитывает circuit state default provider без synthetic provider calls. |
| Provider observability | Requests, 2xx/4xx/5xx, transport errors, in-flight, duration и passive health. |
| Prometheus | Global + provider-labelled metrics на `/_metrics`. |
| Configurable CORS | Disable switch, allow-list, preflight validation, credentials policy и max-age. |
| Charm TUI | Overview, Logs, Clients, Providers, Config и Help. |
| Strict config | Unknown YAML fields отклоняются через `KnownFields(true)`. |
| Legacy migration | Старый `upstream:` автоматически становится provider `gemini`. |

## Архитектура

```text
Client / SDK
    │  GemGate bearer token
    ▼
CORS policy
    ▼
Immutable runtime snapshot
    ├── client auth + rolling rate limit
    ├── provider router + auth adapter
    ├── request/body policy
    └── provider client
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

Runtime и HTTP-часть разделены по ответственности:

- `internal/config` — strict YAML, environment expansion, file-backed secrets, defaults и validation;
- `internal/provider` — provider catalog и auth contract;
- `internal/gateway/gateway.go` — lifecycle и request coordination;
- `internal/gateway/runtime.go` — immutable snapshots и atomic reload;
- `internal/gateway/proxy.go` — routing/auth/upstream streaming;
- `internal/gateway/circuitbreaker.go` — provider circuit state machine;
- `internal/gateway/readiness.go` — passive readiness;
- `internal/gateway/http_helpers.go` — HTTP/redaction helpers;
- `internal/tui` — presentation поверх gateway snapshots;
- `cmd/gemgate` — CLI, watcher и process lifecycle.

Подробнее: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Поддерживаемые provider types

| `type` | Default upstream | Auth |
| --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | native `x-goog-api-key`; OpenAI path — Bearer |
| `openai` | `https://api.openai.com/v1` | Bearer |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` |
| `groq` | `https://api.groq.com/openai/v1` | Bearer |
| `mistral` | `https://api.mistral.ai/v1` | Bearer |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer |
| `deepseek` | `https://api.deepseek.com` | Bearer |
| `xai` | `https://api.x.ai/v1` | Bearer |
| `cohere` | `https://api.cohere.com/v2` | Bearer |
| `openai-compatible` | задаётся в config | Bearer, если задан `api_key` |
| `none` | задаётся в config | без auth |

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

Hot reload включён по умолчанию с интервалом 5 секунд:

```bash
gemgate serve -config config.yaml -reload-interval 5s
```

Выключить polling:

```bash
gemgate serve -config config.yaml -reload-interval 0
```

## Конфигурация

Минимальный пример:

```yaml
server:
  listen: ":8080"
  read_timeout: "30s"
  write_timeout: "0s"
  idle_timeout: "120s"
  public_health: true
  request_body_limit: "32MiB"

  cors:
    enabled: true
    allowed_origins:
      - "http://localhost:3000"
    allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type", "X-Request-ID"]
    allow_credentials: false
    max_age: "10m"

default_provider: gemini

providers:
  - name: gemini
    type: gemini
    api_key: "${GEMINI_API_KEY}"
    timeout: "0s"

clients:
  - name: local-dev
    token: "${GEMGATE_TOKEN}"
    enabled: true
    rate_limit_rpm: 120
```

`write_timeout: "0s"` и provider `timeout: "0s"` обычно удобны для long-lived model streaming.

### File-backed secrets

Вместо inline/environment provider key:

```yaml
providers:
  - name: openai
    type: openai
    api_key_file: "/run/secrets/openai_api_key"
```

Для client token:

```yaml
clients:
  - name: backend
    token_file: "/run/secrets/gemgate_backend_token"
    enabled: true
    rate_limit_rpm: 120
```

Пути могут быть абсолютными или относительными к каталогу `config.yaml`. Secret file читается заново при каждом reload cycle. Пустой файл отклоняется. `api_key` и `api_key_file` одновременно задавать нельзя; то же правило действует для `token`/`token_file`.

Практический способ ротации — заменить secret file атомарно и дождаться следующего reload cycle. Если новый config/secret некорректен, GemGate оставляет последний рабочий runtime целиком.

### Что reloadable

Без restart можно менять:

- providers: состав, `default_provider`, `base_url`, key/file, headers, provider timeout;
- clients: token/file, enabled, RPM limit;
- CORS policy;
- `request_body_limit`;
- `logging.recent`.

Restart требуется для listener-level параметров:

- `server.listen`;
- `server.read_timeout`;
- `server.write_timeout`;
- `server.idle_timeout`.

Это сознательное ограничение: reload не должен частично менять уже запущенный `http.Server`.

### Несколько провайдеров

```yaml
default_provider: openai

providers:
  - name: openai
    type: openai
    api_key_file: "/run/secrets/openai_api_key"

  - name: claude
    type: anthropic
    api_key: "${ANTHROPIC_API_KEY}"

  - name: fast
    type: groq
    api_key: "${GROQ_API_KEY}"

  - name: local
    type: openai-compatible
    base_url: "http://127.0.0.1:11434/v1"
```

## Маршрутизация

```text
/providers/{provider-name}/{provider-path}
```

Префикс provider route удаляется перед отправкой upstream:

```text
POST /providers/openai/responses
  -> https://api.openai.com/v1/responses

POST /providers/claude/v1/messages
  -> https://api.anthropic.com/v1/messages
```

Пути без `/providers/...` идут в `default_provider`.

## Клиентская авторизация

Все приложения используют GemGate token:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

До upstream GemGate удаляет известные provider credential headers, затем selected adapter выставляет серверную авторизацию. GemGate bearer token не должен попадать AI-провайдеру.

## CORS

CORS — браузерная политика, а не authentication.

Production allow-list:

```yaml
server:
  cors:
    enabled: true
    allowed_origins:
      - "https://app.example.com"
      - "https://admin.example.com"
    allowed_methods: ["GET", "POST", "OPTIONS"]
    allowed_headers: ["Authorization", "Content-Type", "X-Request-ID"]
    max_age: "10m"
```

Или отключить:

```yaml
server:
  cors:
    enabled: false
```

`allow_credentials: true` с wildcard origin отклоняется. При отсутствии `server.cors` старые конфиги сохраняют wildcard behavior ради совместимости.

## Rate limiting

`clients[].rate_limit_rpm` — process-local exact sliding window на последние 60 секунд. Он убирает double burst fixed-window limiter, но не является глобальным лимитом для нескольких replicas.

Для общего quota across replicas нужен shared enforcement/backend; это остаётся отдельным архитектурным слоем.

## Circuit breaker

Breaker защищает upstream и budget от повторного шторма при явно падающем provider, но **не повторяет пользовательские запросы**.

Текущая policy:

- failure = transport error или HTTP 5xx;
- HTTP 4xx/429 circuit не открывают;
- после 5 последовательных failures circuit переходит в `open`;
- open period — 30 секунд;
- пока circuit открыт, GemGate отвечает локальным `503 Service Unavailable`, `Retry-After` и `X-GemGate-Circuit: open`, не вызывая upstream;
- после cooldown пропускается ровно один `half_open` probe;
- успешный probe закрывает circuit, неуспешный открывает его ещё на 30 секунд;
- никаких automatic retries generation requests нет.

Circuit state привязан к provider observability и сохраняется при обычном config reload того же provider name.

## Observability и health

### Operational endpoints

| Endpoint | Auth | Назначение |
| --- | --- | --- |
| `/_healthz` | public при `public_health: true`, иначе обычный route policy | process liveness + passive provider health summary |
| `/_readyz` | public при `public_health: true`, иначе GemGate bearer token | passive readiness по circuit state default provider |
| `/_metrics` | GemGate bearer token | Prometheus metrics |
| `/_config` | GemGate bearer token | redacted runtime config |

`/_healthz` остаётся liveness endpoint и не падает только потому, что upstream деградировал.

`/_readyz` — **passive readiness**. Он не делает synthetic provider calls и не расходует quota. Если circuit default provider `open` или `half_open`, endpoint возвращает `503`; проблемы только named/non-default provider не снимают весь gateway с readiness.

Provider health telemetry:

- `unknown` — ещё нет завершённых запросов;
- `healthy` — нет failure streak;
- `warning` — 1–2 transport/5xx failures;
- `degraded` — 3+ failures.

### Prometheus

Экспортируются global и provider-labelled series, включая:

```text
gemgate_provider_requests_total{provider="openai",status_class="2xx"}
gemgate_provider_inflight{provider="openai"}
gemgate_provider_transport_errors_total{provider="openai"}
gemgate_provider_request_duration_seconds_sum{provider="openai"}
gemgate_provider_request_duration_seconds_count{provider="openai"}
gemgate_provider_consecutive_failures{provider="openai"}
```

Provider duration измеряется до EOF/Close response body, поэтому streaming lifetime учитывается целиком.

## TUI

Разделы:

1. **Overview** — traffic, p95, rate limits и provider health;
2. **Logs** — provider-aware request log;
3. **Clients** — usage и RPM limits;
4. **Providers** — type, default role, health, requests, errors, in-flight и duration;
5. **Config** — redacted runtime config и CORS;
6. **Help** — controls и operational notes.

Управление: `1-6`, `Tab`, `Shift+Tab`, `r`, `space/p`, `a/w/e/u`, `?`, `q`.

## Legacy config

Старый Gemini-only config остаётся валиден:

```yaml
upstream:
  base_url: "https://generativelanguage.googleapis.com"
  api_key: "${GEMINI_API_KEY}"
  timeout: "0s"
```

`upstream.api_key_file` также поддерживается.

## Docker / systemd

```bash
docker compose up --build
```

Для production отдавайте secrets через environment/orchestrator credentials или file-backed secrets. Не запекайте ключи в image.

В репозитории есть `gemgate.service`; адаптируйте `WorkingDirectory`, `ExecStart`, Unix user/group и способ доставки секретов.

## Разработка

```bash
gofmt -w .
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

GitHub Actions выполняет эти проверки на каждом push и pull request.

## Production checklist

- Terminate TLS на Caddy/Nginx/Traefik/load balancer/private ingress.
- Используйте отдельный GemGate token для каждого consumer.
- Предпочитайте file-backed/orchestrator secrets для live rotation.
- Явно настройте CORS или отключите его.
- Ограничьте `rate_limit_rpm`, если compromised client может создать расходы.
- Помните: circuit breaker предотвращает новый upstream call, но не делает retry/failover автоматически.
- Используйте `/_healthz` для liveness, `/_readyz` для passive readiness.
- Ограничьте egress для custom `base_url`.
- Оставляйте request/response bodies вне логов по умолчанию.
- Rate limits и recent log ring process-local.

Security: [`SECURITY.md`](SECURITY.md). Audit backlog: [`docs/AUDIT.md`](docs/AUDIT.md).

## Лицензия

См. [`LICENSE`](LICENSE).
