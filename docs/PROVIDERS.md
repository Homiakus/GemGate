# Provider guide

GemGate v0.3 includes provider presets for common hosted APIs while keeping a generic OpenAI-compatible mode for local or less common services.

| Type | Default base URL | Auth injected by GemGate | OpenAI-compatible |
| --- | --- | --- | --- |
| `gemini` | `https://generativelanguage.googleapis.com` | native: `x-goog-api-key`; OpenAI path: Bearer | Yes, under Gemini's `/openai/` compatibility path |
| `openai` | `https://api.openai.com/v1` | Bearer | Yes |
| `anthropic` | `https://api.anthropic.com` | `x-api-key` + `anthropic-version` | Native Anthropic API |
| `groq` | `https://api.groq.com/openai/v1` | Bearer | Yes |
| `mistral` | `https://api.mistral.ai/v1` | Bearer | Yes |
| `openrouter` | `https://openrouter.ai/api/v1` | Bearer | Yes |
| `deepseek` | `https://api.deepseek.com` | Bearer | Yes |
| `xai` | `https://api.x.ai/v1` | Bearer | Yes |
| `cohere` | `https://api.cohere.com/v2` | Bearer | No; use Cohere-native paths |
| `openai-compatible` | user supplied | Bearer when `api_key` is set | Yes |
| `none` | user supplied | none | Depends on upstream |

Run `gemgate providers` to see the catalog compiled into the current binary.

## Examples

### OpenAI

```yaml
- name: openai
  type: openai
  api_key: "${OPENAI_API_KEY}"
```

Client endpoint:

```text
http://localhost:8080/providers/openai/responses
```

### Anthropic

```yaml
- name: anthropic
  type: anthropic
  api_key: "${ANTHROPIC_API_KEY}"
```

Client endpoint:

```text
http://localhost:8080/providers/anthropic/v1/messages
```

GemGate supplies the API key and a default `Anthropic-Version: 2023-06-01`. Override the version explicitly under `headers:` when required.

### Gemini

Native endpoint:

```text
http://localhost:8080/providers/gemini/v1beta/models/<model>:generateContent
```

OpenAI-compatible endpoint:

```text
http://localhost:8080/providers/gemini/v1beta/openai/chat/completions
```

### Local OpenAI-compatible server

```yaml
- name: local
  type: openai-compatible
  base_url: "http://127.0.0.1:11434/v1"
```

Then point an OpenAI-style client to:

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

Do not put client-specific secrets in these headers: they are shared for every request routed to that provider.
