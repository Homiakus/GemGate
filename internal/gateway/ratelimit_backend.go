package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gemgate/internal/config"
)

type rateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Degraded   bool
}

type rateLimitBackend interface {
	Allow(ctx context.Context, key string, limit int, now time.Time) (rateLimitDecision, error)
	Retain(keys map[string]struct{})
	Close() error
	Name() string
}

type memoryRateLimitBackend struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
}

func newMemoryRateLimitBackend() *memoryRateLimitBackend {
	return &memoryRateLimitBackend{windows: make(map[string]*rateWindow)}
}

func (b *memoryRateLimitBackend) Allow(_ context.Context, key string, limit int, now time.Time) (rateLimitDecision, error) {
	if limit <= 0 {
		return rateLimitDecision{Allowed: true}, nil
	}
	b.mu.Lock()
	window := b.windows[key]
	if window == nil {
		window = &rateWindow{}
		b.windows[key] = window
	}
	b.mu.Unlock()
	allowed, retryAfter := window.allow(limit, now)
	return rateLimitDecision{Allowed: allowed, RetryAfter: retryAfter}, nil
}

func (b *memoryRateLimitBackend) Retain(keys map[string]struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key := range b.windows {
		if _, ok := keys[key]; !ok {
			delete(b.windows, key)
		}
	}
}

func (b *memoryRateLimitBackend) Close() error { return nil }
func (b *memoryRateLimitBackend) Name() string { return "memory" }

type rateLimitManager struct {
	backend  rateLimitBackend
	failOpen bool
}

func newRateLimitManager(rt config.Runtime) (*rateLimitManager, error) {
	var backend rateLimitBackend
	switch rt.Config.RateLimit.Backend {
	case "", "memory":
		backend = newMemoryRateLimitBackend()
	case "redis":
		redisBackend, err := newRedisRateLimitBackend(rt.Config.RateLimit.Redis, rt.RateLimitTimeout)
		if err != nil {
			return nil, fmt.Errorf("create redis rate-limit backend: %w", err)
		}
		backend = redisBackend
	default:
		return nil, fmt.Errorf("unsupported rate-limit backend %q", rt.Config.RateLimit.Backend)
	}
	return &rateLimitManager{backend: backend, failOpen: rt.Config.RateLimit.Redis.FailOpen}, nil
}

func (m *rateLimitManager) Allow(ctx context.Context, token string, limit int, now time.Time) (rateLimitDecision, error) {
	if limit <= 0 {
		return rateLimitDecision{Allowed: true}, nil
	}
	decision, err := m.backend.Allow(ctx, rateLimitKey(token), limit, now)
	if err != nil {
		decision.Degraded = true
		if m.failOpen {
			decision.Allowed = true
		}
	}
	return decision, err
}

func (m *rateLimitManager) RetainTokens(tokens map[string]clientAuth) {
	keys := make(map[string]struct{}, len(tokens))
	for token := range tokens {
		keys[rateLimitKey(token)] = struct{}{}
	}
	m.backend.Retain(keys)
}

func (m *rateLimitManager) Close() error { return m.backend.Close() }
func (m *rateLimitManager) Name() string { return m.backend.Name() }
func (m *rateLimitManager) FailOpen() bool { return m.failOpen }

func rateLimitKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "client:" + hex.EncodeToString(sum[:16])
}
