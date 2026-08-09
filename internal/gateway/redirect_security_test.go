package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gemgate/internal/config"
)

func TestProviderRedirectIsReturnedWithoutFollowing(t *testing.T) {
	var attackerCalls atomic.Int64
	var leakedAuth atomic.Bool
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerCalls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Provider-Secret") != "" {
			leakedAuth.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer attacker.Close()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", attacker.URL+"/capture")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer providerServer.Close()

	rt := runtimeForTests([]config.ProviderConfig{{
		Name: "provider", Type: "openai-compatible", BaseURL: providerServer.URL, APIKey: "provider-key",
		Headers: map[string]string{"X-Provider-Secret": "custom-secret"},
	}}, "provider")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Location"); got != attacker.URL+"/capture" {
		t.Fatalf("Location=%q", got)
	}
	if attackerCalls.Load() != 0 {
		t.Fatalf("gateway followed provider redirect %d times", attackerCalls.Load())
	}
	if leakedAuth.Load() {
		t.Fatal("provider credentials leaked to redirect target")
	}
}

func TestSameOriginProviderRedirectIsNotFollowed(t *testing.T) {
	var finalCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/final")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		finalCalls.Add(1)
		_, _ = w.Write([]byte("followed"))
	})
	upstream := httptest.NewServer(mux)
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "provider", Type: "openai-compatible", BaseURL: upstream.URL, APIKey: "provider-key"}}, "provider")
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/start", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)

	if resp.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if finalCalls.Load() != 0 {
		t.Fatalf("same-origin redirect followed %d times", finalCalls.Load())
	}
}
