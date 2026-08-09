package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type rateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
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
	mu      sync.RWMutex
	backend rateLimitBackend
}

func newRateLimitManager() *rateLimitManager {
	return &rateLimitManager{backend: newMemoryRateLimitBackend()}
}

func (m *rateLimitManager) Allow(ctx context.Context, token string, limit int, now time.Time) (rateLimitDecision, error) {
	if limit <= 0 {
		return rateLimitDecision{Allowed: true}, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backend.Allow(ctx, rateLimitKey(token), limit, now)
}

func (m *rateLimitManager) RetainTokens(tokens map[string]clientAuth) {
	keys := make(map[string]struct{}, len(tokens))
	for token := range tokens {
		keys[rateLimitKey(token)] = struct{}{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.backend.Retain(keys)
}

func (m *rateLimitManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backend.Close()
}

func (m *rateLimitManager) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backend.Name()
}

func rateLimitKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "client:" + hex.EncodeToString(sum[:16])
}
