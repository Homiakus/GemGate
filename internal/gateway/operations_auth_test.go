package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemgate/internal/config"
)

func TestDedicatedOperationsAuthSeparatesControlAndDataPlane(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Operations.Token = "operations-secret"
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	clientConfig := httptest.NewRequest(http.MethodGet, "/_config", nil)
	clientConfig.Header.Set("Authorization", "Bearer client-token")
	clientConfigResp := httptest.NewRecorder()
	gw.ServeHTTP(clientConfigResp, clientConfig)
	if clientConfigResp.Code != http.StatusUnauthorized {
		t.Fatalf("client token should not access control plane: status=%d", clientConfigResp.Code)
	}

	opsConfig := httptest.NewRequest(http.MethodGet, "/_config", nil)
	opsConfig.Header.Set("Authorization", "Bearer operations-secret")
	opsConfigResp := httptest.NewRecorder()
	gw.ServeHTTP(opsConfigResp, opsConfig)
	if opsConfigResp.Code != http.StatusOK {
		t.Fatalf("operations token config status=%d body=%s", opsConfigResp.Code, opsConfigResp.Body.String())
	}
	if strings.Contains(opsConfigResp.Body.String(), "operations-secret") {
		t.Fatalf("operations token leaked through safe config: %s", opsConfigResp.Body.String())
	}
	if !strings.Contains(opsConfigResp.Body.String(), `"dedicated_auth":true`) {
		t.Fatalf("dedicated auth status missing: %s", opsConfigResp.Body.String())
	}

	opsProxy := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	opsProxy.Header.Set("Authorization", "Bearer operations-secret")
	opsProxyResp := httptest.NewRecorder()
	gw.ServeHTTP(opsProxyResp, opsProxy)
	if opsProxyResp.Code != http.StatusUnauthorized {
		t.Fatalf("operations token should not proxy application traffic: status=%d", opsProxyResp.Code)
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("operations token reached upstream %d times", upstreamCalls.Load())
	}

	appProxy := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	appProxy.Header.Set("Authorization", "Bearer client-token")
	appProxyResp := httptest.NewRecorder()
	gw.ServeHTTP(appProxyResp, appProxy)
	if appProxyResp.Code != http.StatusOK {
		t.Fatalf("application token proxy status=%d body=%s", appProxyResp.Code, appProxyResp.Body.String())
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("application request upstream calls=%d", upstreamCalls.Load())
	}
}

func TestDedicatedOperationsAuthProtectsPrivateHealth(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Operations.Token = "operations-secret"
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/_healthz", "/_readyz", "/_metrics"} {
		clientReq := httptest.NewRequest(http.MethodGet, path, nil)
		clientReq.Header.Set("Authorization", "Bearer client-token")
		clientResp := httptest.NewRecorder()
		gw.ServeHTTP(clientResp, clientReq)
		if clientResp.Code != http.StatusUnauthorized {
			t.Fatalf("%s client status=%d", path, clientResp.Code)
		}

		opsReq := httptest.NewRequest(http.MethodGet, path, nil)
		opsReq.Header.Set("Authorization", "Bearer operations-secret")
		opsResp := httptest.NewRecorder()
		gw.ServeHTTP(opsResp, opsReq)
		if opsResp.Code != http.StatusOK {
			t.Fatalf("%s operations status=%d body=%s", path, opsResp.Code, opsResp.Body.String())
		}
	}
}

func TestPublicHealthRemainsPublicWithDedicatedOperationsAuth(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Operations.Token = "operations-secret"
	rt.Config.Server.PublicHealth = true
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/_healthz", "/_readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		gw.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("public %s status=%d body=%s", path, resp.Code, resp.Body.String())
		}
	}
}

func TestOperationsTokenHotReloadRotation(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Operations.Token = "ops-old"
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	next := rt
	next.Config.Operations.Token = "ops-new"
	result, err := gw.Reload(next)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected operations token reload to apply")
	}

	oldReq := httptest.NewRequest(http.MethodGet, "/_config", nil)
	oldReq.Header.Set("Authorization", "Bearer ops-old")
	oldResp := httptest.NewRecorder()
	gw.ServeHTTP(oldResp, oldReq)
	if oldResp.Code != http.StatusUnauthorized {
		t.Fatalf("old operations token status=%d", oldResp.Code)
	}

	newReq := httptest.NewRequest(http.MethodGet, "/_config", nil)
	newReq.Header.Set("Authorization", "Bearer ops-new")
	newResp := httptest.NewRecorder()
	gw.ServeHTTP(newResp, newReq)
	if newResp.Code != http.StatusOK {
		t.Fatalf("new operations token status=%d body=%s", newResp.Code, newResp.Body.String())
	}
}
