package provider

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type AuthMode string

const (
	AuthBearer    AuthMode = "bearer"
	AuthGemini    AuthMode = "gemini"
	AuthAnthropic AuthMode = "anthropic"
	AuthNone      AuthMode = "none"
)

type Spec struct {
	Type             string
	DisplayName      string
	DefaultBaseURL   string
	Auth             AuthMode
	RequiresAPIKey   bool
	OpenAICompatible bool
	DefaultHeaders   map[string]string
}

var catalog = map[string]Spec{
	"gemini": {
		Type: "gemini", DisplayName: "Google Gemini", DefaultBaseURL: "https://generativelanguage.googleapis.com",
		Auth: AuthGemini, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"openai": {
		Type: "openai", DisplayName: "OpenAI", DefaultBaseURL: "https://api.openai.com/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"anthropic": {
		Type: "anthropic", DisplayName: "Anthropic Claude", DefaultBaseURL: "https://api.anthropic.com",
		Auth: AuthAnthropic, RequiresAPIKey: true,
		DefaultHeaders: map[string]string{"Anthropic-Version": "2023-06-01"},
	},
	"groq": {
		Type: "groq", DisplayName: "Groq", DefaultBaseURL: "https://api.groq.com/openai/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"mistral": {
		Type: "mistral", DisplayName: "Mistral AI", DefaultBaseURL: "https://api.mistral.ai/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"openrouter": {
		Type: "openrouter", DisplayName: "OpenRouter", DefaultBaseURL: "https://openrouter.ai/api/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"deepseek": {
		Type: "deepseek", DisplayName: "DeepSeek", DefaultBaseURL: "https://api.deepseek.com",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"xai": {
		Type: "xai", DisplayName: "xAI", DefaultBaseURL: "https://api.x.ai/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"cohere": {
		Type: "cohere", DisplayName: "Cohere", DefaultBaseURL: "https://api.cohere.com/v2",
		Auth: AuthBearer, RequiresAPIKey: true,
	},
	"together": {
		Type: "together", DisplayName: "Together AI", DefaultBaseURL: "https://api.together.ai/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"cerebras": {
		Type: "cerebras", DisplayName: "Cerebras Inference", DefaultBaseURL: "https://api.cerebras.ai/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"fireworks": {
		Type: "fireworks", DisplayName: "Fireworks AI", DefaultBaseURL: "https://api.fireworks.ai/inference/v1",
		Auth: AuthBearer, RequiresAPIKey: true, OpenAICompatible: true,
	},
	"openai-compatible": {
		Type: "openai-compatible", DisplayName: "OpenAI-compatible", DefaultBaseURL: "",
		Auth: AuthBearer, RequiresAPIKey: false, OpenAICompatible: true,
	},
	"none": {
		Type: "none", DisplayName: "No-auth HTTP", DefaultBaseURL: "",
		Auth: AuthNone, RequiresAPIKey: false,
	},
}

func NormalizeType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func Lookup(kind string) (Spec, bool) {
	spec, ok := catalog[NormalizeType(kind)]
	return spec, ok
}

func Supported() []Spec {
	out := make([]Spec, 0, len(catalog))
	for _, spec := range catalog {
		out = append(out, cloneSpec(spec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func (s Spec) ApplyHeaders(req *http.Request, apiKey string, custom map[string]string) error {
	for key, value := range s.DefaultHeaders {
		req.Header.Set(key, value)
	}
	for key, value := range custom {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("provider header name cannot be empty")
		}
		req.Header.Set(key, value)
	}

	if strings.TrimSpace(apiKey) == "" {
		if s.RequiresAPIKey {
			return fmt.Errorf("provider %q requires an API key", s.Type)
		}
		return nil
	}

	switch s.Auth {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case AuthGemini:
		if isGeminiOpenAIPath(req.URL.Path) {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Del("X-Goog-Api-Key")
		} else {
			req.Header.Set("X-Goog-Api-Key", apiKey)
			req.Header.Del("Authorization")
		}
	case AuthAnthropic:
		req.Header.Set("X-Api-Key", apiKey)
	case AuthNone:
		return nil
	default:
		return fmt.Errorf("unsupported auth mode %q", s.Auth)
	}
	return nil
}

func isGeminiOpenAIPath(path string) bool {
	p := "/" + strings.TrimPrefix(path, "/")
	return strings.Contains(p, "/openai/")
}

func cloneSpec(in Spec) Spec {
	out := in
	if in.DefaultHeaders != nil {
		out.DefaultHeaders = make(map[string]string, len(in.DefaultHeaders))
		for k, v := range in.DefaultHeaders {
			out.DefaultHeaders[k] = v
		}
	}
	return out
}
