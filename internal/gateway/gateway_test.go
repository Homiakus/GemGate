package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestNamedProviderRoutingAndAuthIsolation(t *testing.T) {
	var gotPath, gotAuth, gotAPIKey, gotVersion, gotCustom string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotVersion = r.Header.Get("Anthropic-Version")
		gotCustom = r.Header.Get("X-Test-Provider")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{
		{Name: "default", Type: "openai-compatible", BaseURL: upstream.URL, APIKey: "default-key"},
		{Name: "claude", Type: "anthropic", BaseURL: upstream.URL, APIKey: "anthropic-secret", Headers: map[string]string{"X-Test-Provider": "claude"}},
	}, "default")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/providers/claude/v1/messages", strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("X-Api-Key", "client-must-not-control-this")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("client Authorization leaked upstream: %q", gotAuth)
	}
	if gotAPIKey != "anthropic-secret" {
		t.Fatalf("X-Api-Key = %q", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q", gotVersion)
	}
	if gotCustom != "claude" {
		t.Fatalf("custom provider header = %q", gotCustom)
	}
}

func TestDefaultProviderPreservesLegacyRootRoute(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "upstream-key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestUnknownProviderReturns404WithoutUpstreamDetails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("upstream should not be called") }))
	defer upstream.Close()
	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/providers/missing/responses", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d", resp.Code)
	}
	if strings.Contains(resp.Body.String(), "missing") {
		t.Fatalf("response leaks internal provider detail: %s", resp.Body.String())
	}
}

func TestSafeConfigListsRedactedProviders(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "super-secret-key"}}, "openai")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/_config", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	providers, ok := payload["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers = %#v", payload["providers"])
	}
	p := providers[0].(map[string]any)
	if p["api_key"] == "super-secret-key" {
		t.Fatal("API key was not redacted")
	}
}

func runtimeForTests(providers []config.ProviderConfig, defaultProvider string) config.Runtime {
	providerTimeouts := make(map[string]time.Duration, len(providers))
	for _, p := range providers {
		providerTimeouts[p.Name] = 0
	}
	return config.Runtime{
		Config: config.Config{
			Server:    config.ServerConfig{Listen: ":0", RequestBodyLimit: "1MiB"},
			Providers: providers, DefaultProvider: defaultProvider,
			Clients: []config.ClientConfig{{Name: "test", Token: "client-token", Enabled: true}},
			Logging: config.LoggingConfig{Recent: 20},
		},
		ProviderTimeouts: providerTimeouts,
		RequestBodyLimit: 1 << 20,
	}
}
