package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const paymentKeyPrefix = "payguard:payment:"

// GetPaymentJSON returns ("", false) on a miss, expiry, or any Redis error.
func (c *Client) GetPaymentJSON(ctx context.Context, paymentID uint64) (string, bool) {
	if c == nil {
		return "", false
	}
	val, err := c.rdb.Get(ctx, paymentKey(paymentID)).Result()
	if err != nil {
		if err != redis.Nil {
			slog.Warn("cache: get failed", "payment_id", paymentID, "err", err)
		}
		return "", false
	}
	return val, true
}

func (c *Client) SetPaymentJSON(ctx context.Context, paymentID uint64, jsonBody string, ttl time.Duration) {
	if c == nil {
		return
	}
	if err := c.rdb.Set(ctx, paymentKey(paymentID), jsonBody, ttl).Err(); err != nil {
		slog.Warn("cache: set failed", "payment_id", paymentID, "err", err)
	}
}

// InvalidatePayment must be called on every write to a payment row.
func (c *Client) InvalidatePayment(ctx context.Context, paymentID uint64) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, paymentKey(paymentID)).Err(); err != nil {
		slog.Warn("cache: invalidate failed", "payment_id", paymentID, "err", err)
	}
}

func paymentKey(id uint64) string {
	return fmt.Sprintf("%s%d", paymentKeyPrefix, id)
}
