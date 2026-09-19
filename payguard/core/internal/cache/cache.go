// Package cache is a Redis-backed read cache and leader lock; nothing that mutates money reads from it.
package cache

import "github.com/redis/go-redis/v9"

type Client struct {
	rdb *redis.Client
}

func New(addr string) *Client {
	return &Client{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}
