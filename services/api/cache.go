package main

// Optional Redis cache layer. When REDIS_URL is set the API caches hot,
// expensive reads (the FYP ranking query) in Redis with a short TTL so a
// multi-node deployment shares the cache; when unset the process runs
// without caching (no fake in-memory stand-in — caching is simply off).
// Every call fails open: a Redis outage never fails a request.

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type cache struct {
	rdb *redis.Client
}

func newCache(redisURL string) *cache {
	if redisURL == "" {
		return nil
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil
	}
	return &cache{rdb: redis.NewClient(opt)}
}

func (c *cache) get(ctx context.Context, key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (c *cache) set(ctx context.Context, key string, val []byte, ttl time.Duration) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_ = c.rdb.Set(ctx, key, val, ttl).Err()
}
