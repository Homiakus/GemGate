package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestSafeConfigDoesNotExposeRedisURL(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	rt := runtimeForTests([]config.ProviderConfig{{Name: "openai", Type: "openai", BaseURL: upstream.URL, APIKey: "provider-key"}}, "openai")
	rt.Config.RateLimit = config.RateLimitConfig{
		Backend: "redis",
		Redis: config.RedisRateLimitConfig{
			URL:       "redis://secret-user:secret-password@127.0.0.1:1/0",
			KeyPrefix: "gemgate:test:",
			Timeout:   "50ms",
		},
	}
	rt.RateLimitTimeout = 50 * time.Millisecond
	gw, err := New(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.rateLimits.Close()

	req := httptest.NewRequest(http.MethodGet, "/_config", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	resp := httptest.NewRecorder()
	gw.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, secret := range []string{"secret-user", "secret-password", "redis://"} {
		if strings.Contains(body, secret) {
			t.Fatalf("safe config exposed Redis connection secret %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"backend":"redis"`) || !strings.Contains(body, `"configured":true`) {
		t.Fatalf("safe config does not expose non-secret Redis status: %s", body)
	}
}
