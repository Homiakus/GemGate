package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemgate/internal/config"
)

func TestOperationsListenerSeparatesApplicationAndControlPlane(t *testing.T) {
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
	gw.IsolateOperationsEndpoints()

	appConfig := httptest.NewRequest(http.MethodGet, "/_config", nil)
	appConfig.Header.Set("Authorization", "Bearer operations-secret")
	appConfigResp := httptest.NewRecorder()
	gw.server.Handler.ServeHTTP(appConfigResp, appConfig)
	if appConfigResp.Code != http.StatusNotFound {
		t.Fatalf("application listener operational status=%d", appConfigResp.Code)
	}

	appProxy := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	appProxy.Header.Set("Authorization", "Bearer client-token")
	appProxyResp := httptest.NewRecorder()
	gw.server.Handler.ServeHTTP(appProxyResp, appProxy)
	if appProxyResp.Code != http.StatusOK {
		t.Fatalf("application proxy status=%d body=%s", appProxyResp.Code, appProxyResp.Body.String())
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("application upstream calls=%d", upstreamCalls.Load())
	}

	opsProxy := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{}`))
	opsProxy.Header.Set("Authorization", "Bearer client-token")
	opsProxyResp := httptest.NewRecorder()
	gw.OperationsHandler().ServeHTTP(opsProxyResp, opsProxy)
	if opsProxyResp.Code != http.StatusNotFound {
		t.Fatalf("operations listener application status=%d", opsProxyResp.Code)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("operations listener reached provider; calls=%d", upstreamCalls.Load())
	}

	opsConfig := httptest.NewRequest(http.MethodGet, "/_config", nil)
	opsConfig.Header.Set("Authorization", "Bearer operations-secret")
	opsConfigResp := httptest.NewRecorder()
	gw.OperationsHandler().ServeHTTP(opsConfigResp, opsConfig)
	if opsConfigResp.Code != http.StatusOK {
		t.Fatalf("operations config status=%d body=%s", opsConfigResp.Code, opsConfigResp.Body.String())
	}
}

func TestOperationsListenerKeepsPublicHealthOnlyOnOperationsPort(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.Server.PublicHealth = true
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	gw.IsolateOperationsEndpoints()

	for _, path := range []string{"/_healthz", "/_readyz"} {
		appReq := httptest.NewRequest(http.MethodGet, path, nil)
		appResp := httptest.NewRecorder()
		gw.server.Handler.ServeHTTP(appResp, appReq)
		if appResp.Code != http.StatusNotFound {
			t.Fatalf("application listener %s status=%d", path, appResp.Code)
		}

		opsReq := httptest.NewRequest(http.MethodGet, path, nil)
		opsResp := httptest.NewRecorder()
		gw.OperationsHandler().ServeHTTP(opsResp, opsReq)
		if opsResp.Code != http.StatusOK {
			t.Fatalf("operations listener %s status=%d body=%s", path, opsResp.Code, opsResp.Body.String())
		}
	}
}
