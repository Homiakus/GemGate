package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestRateWindowAllowsWithinLimit(t *testing.T) {
	w := &rateWindow{}
	now := time.Unix(100, 0)

	if ok, _ := w.allow(2, now); !ok {
		t.Fatal("first request should be allowed")
	}
	if ok, _ := w.allow(2, now.Add(time.Second)); !ok {
		t.Fatal("second request should be allowed")
	}
	if ok, reset := w.allow(2, now.Add(2*time.Second)); ok || reset <= 0 {
		t.Fatalf("third request should be limited with positive reset, ok=%t reset=%s", ok, reset)
	}
}

func TestRateWindowResetsAfterMinute(t *testing.T) {
	w := &rateWindow{}
	now := time.Unix(100, 0)

	if ok, _ := w.allow(1, now); !ok {
		t.Fatal("first request should be allowed")
	}
	if ok, _ := w.allow(1, now.Add(61*time.Second)); !ok {
		t.Fatal("request after window reset should be allowed")
	}
}

func TestGatewayRejectsOverClientRateLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	gw, err := New(config.Runtime{
		Config: config.Config{
			Server: config.ServerConfig{Listen: ":0"},
			Upstream: config.UpstreamConfig{
				BaseURL: upstream.URL,
				APIKey:  "gemini-key",
			},
			Clients: []config.ClientConfig{{
				Name:         "local-dev",
				Token:        "client-token",
				Enabled:      true,
				RateLimitRPM: 1,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	first := httptest.NewRecorder()
	gw.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", first.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	second := httptest.NewRecorder()
	gw.ServeHTTP(second, req)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}
