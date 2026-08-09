# Provider guide

GemGate ships explicit presets for common hosted APIs plus generic modes for local/custom upstreams. Presets define only transport metadata such as base URL, authentication mode and compatibility flag; provider-native request/response schemas remain untouched.

| Type | Default base URL | Auth injected by GemGate | API style |
| --- | --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | native `x-goog-api-key`; OpenAI path Bearer | Gemini native + OpenAI compatibility |
| `openai` | `https://api.openai.com/v1` | Bearer | OpenAI |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` | Anthropic native |
| `groq` | `https://api.groq.com/openai/v1` | Bearer | OpenAI-compatible |
| `mistral` | `https://api.mistral.ai/v1` | Bearer | OpenAI-compatible |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer | OpenAI-compatible |
| `deepseek` | `https://api.deepseek.com` | Bearer | OpenAI-compatible |
| `xai` | `https://api.x.ai/v1` | Bearer | OpenAI-compatible |
| `cohere` | `https://api.cohere.com/v2` | Bearer | Cohere native |
| `together` | `https://api.together.ai/v1` | Bearer | OpenAI-compatible |
| `cerebras` | `https://api.cerebras.ai/v1` | Bearer | OpenAI-compatible |
| `fireworks` | `https://api.fireworks.ai/inference/v1` | Bearer | OpenAI-compatible |
| `openai-compatible` | user supplied | Bearer when `api_key` is set | Generic OpenAI-compatible |
| `none` | user supplied | none | Custom HTTP upstream |

Run `gemgate providers` to inspect the catalog compiled into the current binary.

## Hosted OpenAI-compatible examples

```yaml
providers:
  - name: openai
    type: openai
    api_key_file: "/run/secrets/openai_api_key"

  - name: together
    type: together
    api_key: "${TOGETHER_API_KEY}"

  - name: cerebras
    type: cerebras
    api_key: "${CEREBRAS_API_KEY}"

  - name: fireworks
    type: fireworks
    api_key: "${FIREWORKS_API_KEY}"
```

All four can be reached through the same GemGate named-route pattern:

```text
http://localhost:8080/providers/<name>/<provider-path>
```

For example:

```text
POST /providers/together/chat/completions
POST /providers/cerebras/chat/completions
POST /providers/fireworks/chat/completions
```

GemGate does not rewrite the model field or capability payload. Use model identifiers and endpoint paths supported by the selected provider.

## Anthropic

```yaml
- name: anthropic
  type: anthropic
  api_key: "${ANTHROPIC_API_KEY}"
```

Client endpoint:

```text
http://localhost:8080/providers/anthropic/v1/messages
```

GemGate supplies the API key and default `Anthropic-Version: 2023-06-01`. Override the version explicitly under `headers:` when required.

## Gemini

Native endpoint:

```text
http://localhost:8080/providers/gemini/v1beta/models/<model>:generateContent
```

OpenAI-compatible endpoint:

```text
http://localhost:8080/providers/gemini/v1beta/openai/chat/completions
```

The auth adapter switches between Gemini native key auth and Bearer auth according to the selected path.

## Local OpenAI-compatible server

```yaml
- name: local
  type: openai-compatible
  base_url: "http://127.0.0.1:11434/v1"
```

`api_key` is optional for this generic type. Point an OpenAI-style client to:

```text
http://localhost:8080/providers/local/
```

## Custom headers

Provider headers can be declared server-side:

```yaml
- name: openrouter
  type: openrouter
  api_key: "${OPENROUTER_API_KEY}"
  headers:
    HTTP-Referer: "https://example.com"
    X-Title: "GemGate"
```

Custom headers are shared for every request routed to that configured provider. Treat authorization-like values as secrets.

## Adding another preset

A provider that already exposes an OpenAI-compatible API usually needs only a catalog entry if it uses standard Bearer authentication. A new auth scheme belongs behind a new `AuthMode`; it must not be embedded into routing or the TUI.

Every new preset should add tests for:

1. default base URL;
2. required/optional key behavior;
3. injected auth headers;
4. `OpenAICompatible` classification;
5. provider-specific default headers, if any.
