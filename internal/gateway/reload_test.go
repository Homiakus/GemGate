package gateway

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestReloadRotatesProviderKeyAndClientToken(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	initial := reloadRuntime(upstream.URL, "key-one", "token-one")
	gw, err := New(initial)
	if err != nil {
		t.Fatal(err)
	}

	if status := serveStatus(gw, "token-one"); status != http.StatusOK {
		t.Fatalf("initial status = %d", status)
	}

	next := reloadRuntime(upstream.URL, "key-two", "token-two")
	result, err := gw.Reload(next)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected changed reload")
	}
	if status := serveStatus(gw, "token-one"); status != http.StatusUnauthorized {
		t.Fatalf("old token status = %d", status)
	}
	if status := serveStatus(gw, "token-two"); status != http.StatusOK {
		t.Fatalf("new token status = %d", status)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer key-one" || authHeaders[1] != "Bearer key-two" {
		t.Fatalf("upstream auth headers = %#v", authHeaders)
	}
}

func TestReloadDoesNotChangeInflightRuntime(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	oldUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("old"))
	}))
	defer oldUpstream.Close()
	newUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("new"))
	}))
	defer newUpstream.Close()

	gw, err := New(reloadRuntime(oldUpstream.URL, "old-key", "old-token"))
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() { done <- serveStatus(gw, "old-token") }()
	<-started

	if _, err := gw.Reload(reloadRuntime(newUpstream.URL, "new-key", "new-token")); err != nil {
		t.Fatal(err)
	}
	if status := serveStatus(gw, "new-token"); status != http.StatusCreated {
		t.Fatalf("new runtime status = %d", status)
	}
	close(release)
	if status := <-done; status != http.StatusOK {
		t.Fatalf("inflight old runtime status = %d", status)
	}
}

func TestReloadRejectsListenerChangeAndRetainsState(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	initial := reloadRuntime(upstream.URL, "key", "token")
	gw, err := New(initial)
	if err != nil {
		t.Fatal(err)
	}

	next := reloadRuntime(upstream.URL, "new-key", "new-token")
	next.Config.Server.Listen = ":9999"
	if _, err := gw.Reload(next); err == nil {
		t.Fatal("expected listener change to require restart")
	}
	if status := serveStatus(gw, "token"); status != http.StatusOK {
		t.Fatalf("old runtime was not retained, status=%d", status)
	}
	if status := serveStatus(gw, "new-token"); status != http.StatusUnauthorized {
		t.Fatalf("rejected token unexpectedly active, status=%d", status)
	}
}

func reloadRuntime(baseURL, apiKey, token string) config.Runtime {
	return config.Runtime{
		Config: config.Config{
			Server:          config.ServerConfig{Listen: ":0"},
			DefaultProvider: "openai",
			Providers: []config.ProviderConfig{{
				Name: "openai", Type: "openai", BaseURL: baseURL, APIKey: apiKey,
			}},
			Clients: []config.ClientConfig{{Name: "client", Token: token, Enabled: true, RateLimitRPM: 10}},
		},
		ProviderTimeouts: map[string]time.Duration{"openai": 0},
	}
}

func serveStatus(gw *Gateway, token string) int {
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	return rec.Code
}
