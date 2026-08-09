# GemGate

<p align="center">
  <strong>Multi-provider AI API gateway на Go: серверные provider keys, клиентские токены, streaming, observability, rate limits и Charm TUI.</strong>
</p>

<p align="center">
  <img alt="Go 1.25+" src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white">
  <img alt="Charm TUI" src="https://img.shields.io/badge/TUI-Charm-6d28d9">
  <img alt="Docker ready" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white">
  <img alt="Prometheus metrics" src="https://img.shields.io/badge/Metrics-Prometheus-E6522C?logo=prometheus&logoColor=white">
</p>

GemGate ставится между приложениями и AI-провайдерами. Клиент знает только собственный GemGate bearer token; реальные API keys OpenAI, Gemini, Anthropic и других сервисов остаются на сервере. Gateway выбирает upstream по маршруту, удаляет входные provider credentials, добавляет серверную авторизацию и прозрачно проксирует provider-native payloads и streaming responses.

GemGate **не** пытается превращать разные API в один искусственный формат. OpenAI-compatible endpoints остаются OpenAI-compatible, Anthropic остаётся Anthropic, Gemini native остаётся Gemini native. Это уменьшает поверхность ошибок и позволяет использовать новые возможности провайдеров без ожидания обновления транслятора.

> GemGate не обходит квоты, billing, safety policies или upstream rate limits. Provider HTTP responses передаются клиенту без смыслового переписывания.

## Основные возможности

| Возможность | Что даёт |
| --- | --- |
| Multi-provider routing | Несколько AI upstream в одном процессе через `/providers/{name}/...`. |
| Default provider | Старые root URLs продолжают работать через `default_provider`. |
| Server-side credentials | Реальные provider API keys не выдаются приложениям. |
| Credential isolation | Клиентские `Authorization`, `x-api-key`, `api-key`, `x-goog-api-key` и похожие заголовки не проходят upstream. |
| Provider auth adapters | Bearer, Gemini native/OpenAI mode, Anthropic `x-api-key`, no-auth/custom endpoints. |
| Streaming passthrough | SSE и другие streaming responses пересылаются по мере поступления. |
| Sliding-window rate limit | `rate_limit_rpm` использует точное rolling one-minute окно без fixed-window double burst. |
| Provider observability | Requests, 2xx/4xx/5xx, transport errors, in-flight, duration и passive health по каждому provider. |
| Prometheus | Global + provider-labelled metrics на `/_metrics`. |
| Configurable CORS | Disable switch, origin allow-list, preflight validation, credentials policy и max-age. |
| Charm TUI | Overview, Logs, Clients, Providers, Config и Help. |
| Strict config | YAML unknown fields отклоняются через `KnownFields(true)`. |
| Legacy migration | Старый `upstream:` автоматически нормализуется в provider `gemini`. |

## Архитектура

```text
Client / SDK
    │  GemGate bearer token
    ▼
CORS middleware
    ▼
Client auth ──► sliding-window rate limit
    ▼
Provider router
    ▼
Provider auth adapter
    ▼
Provider metrics transport
    ▼
AI provider
```

Ключевые границы:

- `internal/config` — strict YAML, defaults, validation, CORS policy, legacy migration;
- `internal/provider` — каталог provider presets и auth contract;
- `internal/gateway` — HTTP routing, auth, rate limiting, streaming, metrics, logs и operational endpoints;
- `internal/tui` — только представление runtime snapshots; provider protocol logic сюда не попадает;
- `cmd/gemgate` — CLI и lifecycle composition.

Подробно: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Поддерживаемые provider types

| `type` | Default upstream | Auth |
| --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | native `x-goog-api-key`; OpenAI-compatible path — Bearer |
| `openai` | `https://api.openai.com/v1` | Bearer |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` |
| `groq` | `https://api.groq.com/openai/v1` | Bearer |
| `mistral` | `https://api.mistral.ai/v1` | Bearer |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer |
| `deepseek` | `https://api.deepseek.com` | Bearer |
| `xai` | `https://api.x.ai/v1` | Bearer |
| `cohere` | `https://api.cohere.com/v2` | Bearer |
| `openai-compatible` | задаётся в config | Bearer, если указан `api_key` |
| `none` | задаётся в config | без auth |

Список, встроенный в конкретный бинарник:

```bash
gemgate providers
```

Подробнее: [`docs/PROVIDERS.md`](docs/PROVIDERS.md).

## Быстрый старт

### 1. Создайте конфиг

```bash
cp config.example.yaml config.yaml
```

### 2. Задайте секреты

Linux/macOS:

```bash
export GEMINI_API_KEY="your-provider-key"
export GEMGATE_TOKEN="a-long-random-client-token"
```

PowerShell:

```powershell
$env:GEMINI_API_KEY="your-provider-key"
$env:GEMGATE_TOKEN="a-long-random-client-token"
```

### 3. Запустите

TUI + server:

```bash
go run ./cmd/gemgate run -config config.yaml
```

Headless:

```bash
go run ./cmd/gemgate serve -config config.yaml
```

Build:

```bash
go build -o gemgate ./cmd/gemgate
./gemgate run -config config.yaml
```

## Конфигурация

Минимальный современный пример:

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

logging:
  recent: 300
  log_body: false
  log_headers: false
```

`write_timeout: "0s"` и provider `timeout: "0s"` обычно подходят для long-lived model streaming. Устанавливайте конечные deadlines только если понимаете максимальную длительность generation requests вашего workload.

### Несколько провайдеров

```yaml
default_provider: openai

providers:
  - name: openai
    type: openai
    api_key: "${OPENAI_API_KEY}"

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

`base_url` можно переопределить у любого preset. Для custom endpoints административный config считается доверенной зоной; при необходимости ограничьте egress на уровне host/container/network policy.

## Маршрутизация

### Явный provider

```text
/providers/{provider-name}/{provider-path}
```

Префикс `/providers/{provider-name}` удаляется перед отправкой upstream.

```text
POST /providers/openai/responses
  -> https://api.openai.com/v1/responses

POST /providers/claude/v1/messages
  -> https://api.anthropic.com/v1/messages

POST /providers/gemini/v1beta/openai/chat/completions
  -> https://generativelanguage.googleapis.com/v1beta/openai/chat/completions
```

### Default provider

Любой путь без `/providers/...` проксируется в `default_provider`. Это сохраняет совместимость с v0.2 Gemini-only конфигурацией и позволяет использовать GemGate как drop-in reverse proxy для одного выбранного provider.

## Клиентская авторизация

Все внешние приложения используют только GemGate token:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

До отправки upstream GemGate удаляет известные provider credential headers. Затем selected provider adapter выставляет серверный API key и required default headers.

Эта граница означает, что клиент:

- не может подменить server-side provider key;
- не отправляет свой GemGate token AI-провайдеру;
- не может обойти server-side auth policy собственным `x-api-key`/`api-key`.

## CORS

CORS влияет только на браузерный enforcement. Server-to-server SDK/curl не требуют CORS headers.

Production-варианты:

```yaml
# Browser access from known frontends
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

или полностью выключить CORS:

```yaml
server:
  cors:
    enabled: false
```

Для backward compatibility при полном отсутствии `server.cors` сохраняется старое поведение `allowed_origins: ["*"]`. Новым production deployments рекомендуется явно задавать allow-list либо выключать CORS.

`allow_credentials: true` нельзя использовать вместе с wildcard origin — такой config отклоняется при старте.

## Rate limiting

`clients[].rate_limit_rpm` — process-local exact sliding window на последние 60 секунд.

```yaml
clients:
  - name: dashboard
    token: "${DASHBOARD_TOKEN}"
    enabled: true
    rate_limit_rpm: 60
```

В отличие от fixed-window limiter, клиент не может отправить полный минутный quota непосредственно до границы окна и ещё один полный quota сразу после неё.

Для нескольких replicas лимиты пока не координируются. Если нужен глобальный quota, потребуется shared backend (например Redis); это намеренно не скрывается за локальным счётчиком.

## Observability

### Operational endpoints

| Endpoint | Auth | Назначение |
| --- | --- | --- |
| `/_healthz` | публичный только при `public_health: true` | process liveness + passive provider health summary |
| `/_metrics` | GemGate bearer token | Prometheus metrics |
| `/_config` | GemGate bearer token | redacted runtime config |

Provider health — **passive**, а не активная readiness probe:

- `unknown` — provider ещё не завершил запросы;
- `healthy` — нет текущего failure streak;
- `warning` — 1–2 последовательных transport/5xx failures;
- `degraded` — 3+ последовательных failures.

Успешный/non-5xx response сбрасывает failure streak. GemGate не делает скрытых provider requests только ради health check.

### Prometheus

Кроме global counters экспортируются provider-labelled series:

```text
gemgate_provider_requests_total{provider="openai",status_class="2xx"}
gemgate_provider_inflight{provider="openai"}
gemgate_provider_transport_errors_total{provider="openai"}
gemgate_provider_request_duration_seconds_sum{provider="openai"}
gemgate_provider_request_duration_seconds_count{provider="openai"}
gemgate_provider_consecutive_failures{provider="openai"}
```

Provider duration измеряется до EOF/Close response body, поэтому включает реальную длительность streaming response, а не только time-to-headers.

## TUI

TUI теперь разделён на небольшие view-модули и не содержит provider protocol logic.

Разделы:

1. **Overview** — traffic, p95, rate limits и provider health summary;
2. **Logs** — request log с отдельной колонкой `Provider`;
3. **Clients** — usage и RPM limits;
4. **Providers** — type, default role, passive health, requests, error rate, in-flight и average duration;
5. **Config** — redacted runtime config, providers и CORS policy;
6. **Help** — controls и operational notes.

Управление: `1-6`, `Tab`, `Shift+Tab`, `r`, `space/p`, `a/w/e/u`, `?`, `q`.

Старые изображения в `docs/assets` сохранены как исторические материалы; фактический v0.3 layout определяется текущим TUI-кодом.

## Legacy config

Старый single-Gemini config продолжает работать:

```yaml
upstream:
  base_url: "https://generativelanguage.googleapis.com"
  api_key: "${GEMINI_API_KEY}"
  timeout: "0s"
```

При `config.Load` он нормализуется в provider `gemini`. Для новых deployments используйте `providers:` и `default_provider:`.

## Docker

```bash
docker compose up --build
```

Не запекайте реальные provider keys в image. Передавайте secrets через environment, orchestrator secrets или внешний secret manager.

## Systemd

В репозитории есть `gemgate.service`. Перед production use адаптируйте `WorkingDirectory`, `ExecStart`, Unix user/group и механизм передачи секретов.

## Разработка

```bash
gofmt -w .
go vet ./...
go test -race -cover ./...
go build ./cmd/gemgate
```

GitHub Actions выполняет эти проверки на push и pull request.

## Production checklist

- Terminate TLS на Caddy/Nginx/Traefik/load balancer/private ingress.
- Используйте отдельный GemGate token для каждого consumer.
- Явно настройте CORS или отключите его, если browser access не нужен.
- Ограничьте `rate_limit_rpm`, если compromised client может создать существенные расходы.
- Ограничьте egress для deployments с user-managed custom `base_url`.
- Оставляйте request/response bodies вне логов по умолчанию.
- Помните, что rate limits и recent log ring process-local.
- Не воспринимайте passive provider health как circuit breaker или readiness guarantee.

Security notes: [`SECURITY.md`](SECURITY.md). Текущий реестр аудита: [`docs/AUDIT.md`](docs/AUDIT.md).

## Лицензия

См. [`LICENSE`](LICENSE).
