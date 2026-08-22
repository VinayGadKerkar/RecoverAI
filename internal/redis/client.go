package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps go-redis with convenience methods used across the system.
type Client struct {
	rdb *redis.Client
}

// NewClient creates and pings a Redis client.
func NewClient(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	rdb := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Close shuts down the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// SetNXWithTTL sets key=value only if key does not exist.
// Returns true if the key already existed (i.e., was NOT set — duplicate).
func (c *Client) SetNXWithTTL(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	set, err := c.rdb.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, err
	}
	// SetNX returns true when the key was newly set, false when it already existed
	return !set, nil // invert: true means "already existed" (duplicate)
}

// Get retrieves a string value.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Set stores a key-value pair with TTL.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Incr atomically increments a counter and returns the new value.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.rdb.Incr(ctx, key).Result()
}

// Expire sets a TTL on an existing key.
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// Del removes one or more keys.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// IncrBy atomically increments a counter by delta.
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return c.rdb.IncrBy(ctx, key, delta).Result()
}

// Exists reports whether a key exists.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

// Unwrap returns the underlying go-redis client for advanced usage.
func (c *Client) Unwrap() *redis.Client {
	return c.rdb
}
