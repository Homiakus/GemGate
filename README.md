# GemGate

<p align="center">
  <strong>Лёгкий multi-provider AI API gateway на Go: серверные provider keys, клиентские токены, streaming, rate limits, Prometheus и Charm TUI.</strong>
</p>

<p align="center">
  <img alt="Go 1.25+" src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white">
  <img alt="Charm TUI" src="https://img.shields.io/badge/TUI-Charm-6d28d9">
  <img alt="Docker ready" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white">
  <img alt="Prometheus metrics" src="https://img.shields.io/badge/Metrics-Prometheus-E6522C?logo=prometheus&logoColor=white">
</p>

GemGate ставится между приложениями и AI-провайдерами. Клиенты знают только собственный GemGate bearer token; реальные API keys OpenAI, Gemini, Anthropic и других сервисов остаются на сервере. Gateway выбирает провайдера по маршруту, удаляет клиентские credential-заголовки, добавляет upstream auth и проксирует тело запроса/ответа без попыток «унифицировать» разные API-схемы.

> GemGate не обходит квоты, биллинг, safety policies или rate limits провайдеров. Upstream HTTP-ответы передаются клиенту как есть.

## Что изменилось в v0.3

- вместо одного жестко заданного Gemini upstream — реестр нескольких провайдеров;
- маршруты `/providers/{name}/...` с сохранением старого root-route через `default_provider`;
- встроенные presets: Gemini, OpenAI, Anthropic, Groq, Mistral, OpenRouter, DeepSeek, xAI, Cohere;
- generic `openai-compatible` и `none` для локальных/нестандартных endpoints;
- provider-specific auth без утечки клиентского GemGate token upstream;
- строгий YAML (`KnownFields`) — опечатки в полях теперь не игнорируются молча;
- `config.example.yaml`, которого раньше не хватало;
- архитектурная документация, provider guide, security notes и GitHub Actions CI;
- дополнительные тесты маршрутизации, auth isolation, redaction и backward compatibility.

## Возможности

| Возможность | Что дает |
| --- | --- |
| Multi-provider routing | Несколько AI upstream в одном процессе. |
| Серверные provider keys | Реальные API keys не выдаются клиентам. |
| Клиентские токены | Отдельный `Authorization: Bearer ...` для каждого потребителя GemGate. |
| Provider auth adapters | Bearer, Gemini native/OpenAI mode, Anthropic `x-api-key`. |
| OpenAI-compatible providers | OpenAI, Groq, Mistral, OpenRouter, DeepSeek, xAI и custom endpoints. |
| Native provider APIs | Gemini native, Anthropic Messages, Cohere и любые passthrough paths. |
| Streaming passthrough | SSE/streaming response пересылается по мере поступления. |
| Per-client rate limit | `clients[].rate_limit_rpm` защищает квоты и бюджет. |
| Charm TUI | Overview, logs, clients, routes, config, hotkeys. |
| Prometheus | `/_metrics` под клиентской авторизацией. |
| Redacted config | `/_config` показывает runtime-конфиг без полных секретов. |
| Legacy migration | Старый `upstream:` автоматически становится provider `gemini`. |

## Поддерживаемые provider types

| `type` | Default upstream | Auth |
| --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | native `x-goog-api-key`, OpenAI path — Bearer |
| `openai` | `https://api.openai.com/v1` | Bearer |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` |
| `groq` | `https://api.groq.com/openai/v1` | Bearer |
| `mistral` | `https://api.mistral.ai/v1` | Bearer |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer |
| `deepseek` | `https://api.deepseek.com` | Bearer |
| `xai` | `https://api.x.ai/v1` | Bearer |
| `cohere` | `https://api.cohere.com/v2` | Bearer |
| `openai-compatible` | задаётся пользователем | Bearer, если указан `api_key` |
| `none` | задаётся пользователем | без auth |

В бинарнике список можно получить командой:

```bash
gemgate providers
```

Подробности: [`docs/PROVIDERS.md`](docs/PROVIDERS.md).

## Быстрый старт

### 1. Создайте конфиг

```bash
cp config.example.yaml config.yaml
```

### 2. Задайте секреты

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

```bash
go run ./cmd/gemgate run -config config.yaml
```

Headless:

```bash
go run ./cmd/gemgate serve -config config.yaml
```

## Конфигурация

Минимальная современная конфигурация:

```yaml
server:
  listen: ":8080"
  read_timeout: "30s"
  write_timeout: "0s"
  idle_timeout: "120s"
  public_health: true
  request_body_limit: "32MiB"

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

`base_url` можно переопределять у любого provider preset. `timeout: "0s"` означает отсутствие общего deadline на upstream request — обычно это правильнее для долгой генерации и streaming.

### Legacy config

Старый вариант продолжает работать:

```yaml
upstream:
  base_url: "https://generativelanguage.googleapis.com"
  api_key: "${GEMINI_API_KEY}"
  timeout: "0s"
```

Он нормализуется в provider с именем `gemini`. Для новых конфигураций используйте `providers:`.

## Маршрутизация

### Явный provider

```text
/providers/{provider-name}/{provider-path}
```

Префикс `/providers/{provider-name}` удаляется перед отправкой upstream.

Примеры:

```text
POST /providers/openai/responses
  -> https://api.openai.com/v1/responses

POST /providers/anthropic/v1/messages
  -> https://api.anthropic.com/v1/messages

POST /providers/gemini/v1beta/openai/chat/completions
  -> https://generativelanguage.googleapis.com/v1beta/openai/chat/completions
```

### Default provider

Любой путь без `/providers/...` проксируется в `default_provider`. Благодаря этому старые Gemini base URLs остаются рабочими.

## Клиентская авторизация

Все внешние клиенты используют только GemGate token:

```http
Authorization: Bearer <GEMGATE_TOKEN>
```

Перед upstream-запросом GemGate удаляет входные `Authorization`, `x-api-key`, `api-key`, `x-goog-api-key` и другие credential-заголовки. Затем выбранный provider adapter добавляет серверный API key.

Это принципиальная граница безопасности: клиент не может подменить provider key и GemGate token не должен оказаться у upstream-сервиса.

## Примеры API

### OpenAI Responses API

```bash
curl "http://localhost:8080/providers/openai/responses" \
  -H "Authorization: Bearer $GEMGATE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"<openai-model>","input":"Say hello in one sentence."}'
```

Для OpenAI SDK используйте provider route как base URL:

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-gemgate-token",
    base_url="http://localhost:8080/providers/openai/",
)

response = client.responses.create(
    model="<openai-model>",
    input="Say hello in one sentence.",
)
```

### Anthropic Messages API

```bash
curl "http://localhost:8080/providers/anthropic/v1/messages" \
  -H "Authorization: Bearer $GEMGATE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<claude-model>",
    "max_tokens": 256,
    "messages": [{"role":"user","content":"Hello"}]
  }'
```

Клиент не должен передавать реальный `x-api-key`: GemGate выставит его сам.

### Gemini OpenAI compatibility

```text
http://localhost:8080/providers/gemini/v1beta/openai/
```

### Gemini native

```text
http://localhost:8080/providers/gemini/v1beta/models/<model>:generateContent
```

## Operational endpoints

| Endpoint | Auth | Назначение |
| --- | --- | --- |
| `/_healthz` | публичный только при `public_health: true` | health probe |
| `/_metrics` | GemGate bearer token | Prometheus metrics |
| `/_config` | GemGate bearer token | redacted runtime config |

## TUI

### Overview

![GemGate TUI overview](docs/assets/tui-overview.png)

### Logs

![GemGate TUI logs](docs/assets/tui-logs.png)

### Clients / Routes

![GemGate TUI clients](docs/assets/tui-clients-routes.png)

![GemGate TUI routes](docs/assets/tui-routes.png)

Управление: `1-6`, `Tab`, `Shift+Tab`, `r`, `space/p`, `a/w/e/u`, `?`, `q`.

> Текущий first-time wizard остаётся намеренно простым и создаёт Gemini-конфигурацию. После первого запуска добавьте остальные providers в YAML. Provider protocol logic находится не в TUI, а в `internal/provider`.

## Docker

```bash
docker compose up --build
```

Перед запуском подготовьте `config.yaml` и передайте секреты через environment/secrets. Не запекайте реальные provider keys в image.

## Systemd

В репозитории есть `gemgate.service`; адаптируйте `WorkingDirectory`, `ExecStart`, пользователя и способ доставки секретов под свою систему.

## Разработка

```bash
gofmt -w .
go vet ./...
go test -race ./...
go build ./cmd/gemgate
```

CI выполняет те же базовые проверки на push/PR.

Архитектура и правила расширения: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Production notes

- Завершайте TLS на Caddy, Nginx, Traefik, cloud load balancer или private ingress.
- Выдавайте отдельный GemGate token каждому приложению.
- Ограничивайте `rate_limit_rpm`, если компрометация клиента может привести к расходам.
- Для custom `base_url` учитывайте SSRF/egress policy: конфигурация считается доверенной администраторской зоной.
- `write_timeout: "0s"` обычно нужен для long-lived SSE.
- Логи хранятся в памяти текущего процесса; для долговременного аудита используйте внешний logging layer.
- Rate limits также in-memory и не координируются между несколькими replicas.

См. [`SECURITY.md`](SECURITY.md).

## Лицензия

См. [`LICENSE`](LICENSE).
