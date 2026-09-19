package cache

import (
	"context"
	"log/slog"
	"time"
)

const lockKeyPrefix = "payguard:lock:"

// TryLock fails open on any Redis error.
func (c *Client) TryLock(ctx context.Context, key string, ttl time.Duration) bool {
	if c == nil {
		return true
	}
	ok, err := c.rdb.SetNX(ctx, lockKeyPrefix+key, "1", ttl).Result()
	if err != nil {
		slog.Warn("cache: lock acquisition failed, proceeding without it", "key", key, "err", err)
		return true
	}
	return ok
}

func (c *Client) Unlock(ctx context.Context, key string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, lockKeyPrefix+key).Err(); err != nil {
		slog.Warn("cache: unlock failed", "key", key, "err", err)
	}
}
