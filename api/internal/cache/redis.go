package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

// Initialize connects to the Redis instance
func Initialize(ctx context.Context, addr string) error {
	Client = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "", // no password set
		DB:           0,  // use default DB
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		PoolSize:     1000,
	})

	if err := Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis at %s: %v", addr, err)
	}

	log.Printf("Connected to Redis at %s", addr)
	return nil
}

// Close terminates the Redis connection
func Close() {
	if Client != nil {
		Client.Close()
	}
}

// IncrementTotalRows increments the global row counter by the specified amount
func IncrementTotalRows(ctx context.Context, count int64) error {
	if Client == nil {
		return fmt.Errorf("Redis client not initialized")
	}
	return Client.IncrBy(ctx, "stats:total_rows", count).Err()
}

// GetTotalRows retrieves the total number of rows uploaded
func GetTotalRows(ctx context.Context) (int64, error) {
	if Client == nil {
		return 0, fmt.Errorf("Redis client not initialized")
	}
	val, err := Client.Get(ctx, "stats:total_rows").Int64()
	if err == redis.Nil {
		return 0, nil // Key doesn't exist yet
	}
	return val, err
}
