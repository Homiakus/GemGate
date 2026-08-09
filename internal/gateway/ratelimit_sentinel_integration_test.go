package gateway

import (
	"context"
	"os"
	"testing"
	"time"

	"gemgate/internal/config"

	"github.com/redis/go-redis/v9"
)

func TestRedisSentinelFailoverKeepsLimiterAvailable(t *testing.T) {
	redisURL := os.Getenv("GEMGATE_TEST_SENTINEL_URL")
	sentinelAddr := os.Getenv("GEMGATE_TEST_SENTINEL_ADDR")
	if redisURL == "" || sentinelAddr == "" {
		t.Skip("Sentinel integration environment is not configured")
	}

	const masterName = "gemgate-master"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	sentinel := redis.NewSentinelClient(&redis.Options{
		Addr:         sentinelAddr,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	defer sentinel.Close()

	waitUntil(t, ctx, 250*time.Millisecond, func() bool {
		if err := sentinel.CkQuorum(ctx, masterName).Err(); err != nil {
			return false
		}
		replicas, err := sentinel.Replicas(ctx, masterName).Result()
		return err == nil && len(replicas) >= 1
	}, "Sentinel quorum and replica discovery")

	before, err := sentinel.GetMasterAddrByName(ctx, masterName).Result()
	if err != nil || len(before) != 2 {
		t.Fatalf("resolve initial master: addr=%v err=%v", before, err)
	}

	prefix := "gemgate:sentinel-e2e:" + time.Now().Format("150405.000000000") + ":"
	backend, err := newRedisRateLimitBackend(config.RedisRateLimitConfig{
		URL:       redisURL,
		KeyPrefix: prefix,
		Timeout:   "1s",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if backend.Mode() != "sentinel" {
		t.Fatalf("backend mode=%q", backend.Mode())
	}

	first, err := backend.Allow(ctx, rateLimitKey("persistent-token"), 1, time.Now())
	if err != nil || !first.Allowed {
		t.Fatalf("initial limiter decision=%#v err=%v", first, err)
	}

	// Give the replica a short opportunity to apply the limiter write before the
	// forced promotion. The post-failover assertion below still verifies that the
	// state survived promotion rather than merely checking connectivity.
	time.Sleep(500 * time.Millisecond)

	if err := sentinel.Failover(ctx, masterName).Err(); err != nil {
		t.Fatalf("force Sentinel failover: %v", err)
	}

	waitUntil(t, ctx, 250*time.Millisecond, func() bool {
		after, err := sentinel.GetMasterAddrByName(ctx, masterName).Result()
		return err == nil && len(after) == 2 && (after[0] != before[0] || after[1] != before[1])
	}, "Sentinel master promotion")

	var persistent rateLimitDecision
	waitUntil(t, ctx, 250*time.Millisecond, func() bool {
		decision, allowErr := backend.Allow(ctx, rateLimitKey("persistent-token"), 1, time.Now())
		if allowErr != nil {
			return false
		}
		persistent = decision
		return true
	}, "existing failover client reconnect")
	if persistent.Allowed || persistent.RetryAfter <= 0 {
		t.Fatalf("quota state did not survive Sentinel promotion: %#v", persistent)
	}

	fresh, err := backend.Allow(ctx, rateLimitKey("fresh-token"), 1, time.Now())
	if err != nil || !fresh.Allowed {
		t.Fatalf("post-failover fresh-token decision=%#v err=%v", fresh, err)
	}
}

func waitUntil(t *testing.T, ctx context.Context, interval time.Duration, fn func() bool, what string) {
	t.Helper()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v", what, ctx.Err())
		case <-ticker.C:
		}
	}
}
