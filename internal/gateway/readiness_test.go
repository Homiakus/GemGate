package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestReadinessRequiresAuthUnlessPublicHealthIsEnabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	gw, err := New(reloadRuntime(upstream.URL, "key", "token"))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_readyz", nil)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated readiness status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/_readyz", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-GemGate-Readiness") != "passive" {
		t.Fatalf("authenticated readiness status=%d headers=%v", rec.Code, rec.Header())
	}
}

func TestReadinessFailsWhenDefaultProviderCircuitIsOpen(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rt := reloadRuntime(upstream.URL, "key", "token")
	rt.Config.Server.PublicHealth = true
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	breaker := gw.currentRuntime().providers["openai"].breaker
	now := time.Now()
	for i := 0; i < defaultCircuitFailureThreshold; i++ {
		at := now.Add(time.Duration(i) * time.Millisecond)
		permit, ok, _ := breaker.allow(at)
		if !ok {
			t.Fatalf("failure %d unexpectedly rejected before threshold", i)
		}
		breaker.finish(permit, true, at)
	}

	req := httptest.NewRequest(http.MethodGet, "/_readyz", nil)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestDisabledDefaultCircuitDoesNotFailReadiness(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	rt := reloadRuntime(upstream.URL, "key", "token")
	rt.Config.Server.PublicHealth = true
	rt.ProviderCircuits = map[string]config.CircuitBreakerRuntime{
		"openai": {Enabled: false, FailureThreshold: 1, OpenFor: time.Second},
	}
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/models", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/_readyz", nil)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled circuit readiness status = %d", rec.Code)
	}
}
