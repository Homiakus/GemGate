package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"gemgate/internal/config"

	"github.com/redis/go-redis/v9"
)

var redisRateScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local member = ARGV[2]
local tm = redis.call('TIME')
local now = tonumber(tm[1]) * 1000 + math.floor(tonumber(tm[2]) / 1000)
local cutoff = now - 60000
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  local retry = 1
  if oldest[2] then
    retry = math.max(1, 60000 - (now - tonumber(oldest[2])))
  end
  redis.call('PEXPIRE', key, 61000)
  return {0, retry}
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, 61000)
return {1, 0}
`)

type redisRateLimitBackend struct {
	client   *redis.Client
	prefix   string
	instance string
	seq      atomic.Uint64
	mode     string
}

func newRedisRateLimitBackend(cfg config.RedisRateLimitConfig, timeout time.Duration) (*redisRateLimitBackend, error) {
	client, mode, err := newRedisRateLimitClient(cfg.URL, timeout)
	if err != nil {
		return nil, err
	}
	return &redisRateLimitBackend{
		client:   client,
		prefix:   cfg.KeyPrefix,
		instance: redisInstanceID(),
		mode:     mode,
	}, nil
}

func redisRateLimitMode(redisURL string) string {
	u, err := url.Parse(redisURL)
	if err != nil {
		return "invalid"
	}
	if u.Query().Get("master_name") != "" {
		return "sentinel"
	}
	return "standalone"
}

func newRedisRateLimitClient(redisURL string, timeout time.Duration) (*redis.Client, string, error) {
	mode := redisRateLimitMode(redisURL)
	if mode == "invalid" {
		return nil, "", fmt.Errorf("invalid Redis URL")
	}
	if mode == "sentinel" {
		opts, err := redis.ParseFailoverURL(redisURL)
		if err != nil {
			return nil, "", fmt.Errorf("parse Redis Sentinel failover URL: %w", err)
		}
		if opts.MasterName == "" {
			return nil, "", fmt.Errorf("Redis Sentinel URL requires master_name")
		}
		opts.DialTimeout = timeout
		opts.ReadTimeout = timeout
		opts.WriteTimeout = timeout
		return redis.NewFailoverClient(opts), mode, nil
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, "", err
	}
	opts.DialTimeout = timeout
	opts.ReadTimeout = timeout
	opts.WriteTimeout = timeout
	return redis.NewClient(opts), mode, nil
}

func (b *redisRateLimitBackend) Allow(ctx context.Context, key string, limit int, _ time.Time) (rateLimitDecision, error) {
	if limit <= 0 {
		return rateLimitDecision{Allowed: true}, nil
	}
	member := b.instance + ":" + strconv.FormatUint(b.seq.Add(1), 10)
	result, err := redisRateScript.Run(ctx, b.client, []string{b.prefix + key}, limit, member).Slice()
	if err != nil {
		return rateLimitDecision{}, err
	}
	if len(result) != 2 {
		return rateLimitDecision{}, fmt.Errorf("unexpected redis limiter result length %d", len(result))
	}
	allowed, err := redisInt64(result[0])
	if err != nil {
		return rateLimitDecision{}, fmt.Errorf("decode redis limiter allowed: %w", err)
	}
	retryMS, err := redisInt64(result[1])
	if err != nil {
		return rateLimitDecision{}, fmt.Errorf("decode redis limiter retry: %w", err)
	}
	return rateLimitDecision{Allowed: allowed == 1, RetryAfter: time.Duration(retryMS) * time.Millisecond}, nil
}

func (b *redisRateLimitBackend) Retain(map[string]struct{}) {}
func (b *redisRateLimitBackend) Close() error               { return b.client.Close() }
func (b *redisRateLimitBackend) Name() string               { return "redis" }
func (b *redisRateLimitBackend) Mode() string               { return b.mode }

func redisInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected %T", value)
	}
}

func redisInstanceID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}
