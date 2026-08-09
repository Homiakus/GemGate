package provider

import (
	"net/http"
	"testing"
)

func TestGeminiAuthModes(t *testing.T) {
	spec, _ := Lookup("gemini")

	native, _ := http.NewRequest(http.MethodPost, "https://example.test/v1beta/models/x:generateContent", nil)
	if err := spec.ApplyHeaders(native, "secret", nil); err != nil {
		t.Fatal(err)
	}
	if got := native.Header.Get("X-Goog-Api-Key"); got != "secret" {
		t.Fatalf("native Gemini key = %q", got)
	}
	if got := native.Header.Get("Authorization"); got != "" {
		t.Fatalf("native Gemini Authorization should be empty, got %q", got)
	}

	compat, _ := http.NewRequest(http.MethodPost, "https://example.test/v1beta/openai/chat/completions", nil)
	if err := spec.ApplyHeaders(compat, "secret", nil); err != nil {
		t.Fatal(err)
	}
	if got := compat.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("compat Authorization = %q", got)
	}
}

func TestAnthropicDefaultsAndOverride(t *testing.T) {
	spec, _ := Lookup("anthropic")
	req, _ := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", nil)
	if err := spec.ApplyHeaders(req, "secret", map[string]string{"Anthropic-Version": "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Api-Key"); got != "secret" {
		t.Fatalf("X-Api-Key = %q", got)
	}
	if got := req.Header.Get("Anthropic-Version"); got != "2026-01-01" {
		t.Fatalf("Anthropic-Version = %q", got)
	}
}

func TestOpenAICompatibleAllowsNoKey(t *testing.T) {
	spec, _ := Lookup("openai-compatible")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:11434/v1/models", nil)
	if err := spec.ApplyHeaders(req, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("unexpected Authorization %q", got)
	}
}
