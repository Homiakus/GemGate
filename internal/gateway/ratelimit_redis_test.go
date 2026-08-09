package gateway

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gemgate/internal/config"
)

func TestRateLimitKeyDoesNotExposeToken(t *testing.T) {
	token := "super-secret-client-token"
	key := rateLimitKey(token)
	if strings.Contains(key, token) {
		t.Fatal("rate-limit key exposes bearer token")
	}
	if !strings.HasPrefix(key, "client:") {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestRedisRateLimitIsSharedAcrossInstances(t *testing.T) {
	redisURL := os.Getenv("GEMGATE_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("GEMGATE_TEST_REDIS_URL is not set")
	}
	prefix := "gemgate:test:" + strings.ReplaceAll(t.Name(), "/", ":") + ":" + time.Now().Format("150405.000000000") + ":"
	rt := config.Runtime{
		Config: config.Config{RateLimit: config.RateLimitConfig{
			Backend: "redis",
			Redis: config.RedisRateLimitConfig{URL: redisURL, KeyPrefix: prefix, Timeout: "1s"},
		}},
		RateLimitTimeout: time.Second,
	}
	first, err := newRateLimitManager(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := newRateLimitManager(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	decision, err := first.Allow(context.Background(), "shared-token", 1, time.Now())
	if err != nil || !decision.Allowed {
		t.Fatalf("first decision = %#v err=%v", decision, err)
	}
	decision, err = second.Allow(context.Background(), "shared-token", 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("second instance should see shared quota, decision=%#v", decision)
	}

	decision, err = second.Allow(context.Background(), "different-token", 1, time.Now())
	if err != nil || !decision.Allowed {
		t.Fatalf("different token should have independent quota, decision=%#v err=%v", decision, err)
	}
}

func TestRedisRateLimitFailOpen(t *testing.T) {
	rt := config.Runtime{
		Config: config.Config{RateLimit: config.RateLimitConfig{
			Backend: "redis",
			Redis: config.RedisRateLimitConfig{URL: "redis://127.0.0.1:1/0", KeyPrefix: "gemgate:test:failopen:", Timeout: "50ms", FailOpen: true},
		}},
		RateLimitTimeout: 50 * time.Millisecond,
	}
	manager, err := newRateLimitManager(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	decision, err := manager.Allow(ctx, "token", 1, time.Now())
	if err == nil {
		t.Fatal("expected redis backend error")
	}
	if !decision.Allowed || !decision.Degraded {
		t.Fatalf("fail-open decision = %#v", decision)
	}
}
