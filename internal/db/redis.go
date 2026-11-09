package db

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// OpenRedis membuka koneksi Redis opsional untuk rate limiter terpusat.
// Pemanggil dapat memilih fallback in-memory bila Redis tidak tersedia.
func OpenRedis(ctx context.Context, redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}
