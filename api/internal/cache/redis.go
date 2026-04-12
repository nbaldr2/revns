package cache

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client
var cacheMetrics *CacheMetrics

// CacheMetrics tracks cache performance statistics
type CacheMetrics struct {
	mu        sync.RWMutex
	Hits      int64 `json:"hits"`
	Misses    int64 `json:"misses"`
	Sets      int64 `json:"sets"`
	Deletes   int64 `json:"deletes"`
	Errors    int64 `json:"errors"`
	startTime time.Time
}

// RedisConfig holds configurable Redis settings
type RedisConfig struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxConnAge   time.Duration
}

// DefaultRedisConfig returns optimized default configuration
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Addr:         "127.0.0.1:6379",
		Password:     "",
		DB:           0,
		PoolSize:     100, // More reasonable default
		MinIdleConns: 10,  // Maintain minimum idle connections
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		MaxConnAge:   30 * time.Minute, // Recycle connections periodically
	}
}

// Initialize connects to the Redis instance with optimized settings
func Initialize(ctx context.Context, addr string) error {
	cfg := DefaultRedisConfig()
	if addr != "" {
		cfg.Addr = addr
	}

	// Override with environment variables if set
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DB = n
		}
	}
	if v := os.Getenv("REDIS_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PoolSize = n
		}
	}
	if v := os.Getenv("REDIS_MIN_IDLE_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MinIdleConns = n
		}
	}

	return InitializeWithConfig(ctx, cfg)
}

// InitializeWithConfig creates a Redis client with explicit configuration
func InitializeWithConfig(ctx context.Context, cfg RedisConfig) error {
	Client = redis.NewClient(&redis.Options{
		Addr:            cfg.Addr,
		Password:        cfg.Password,
		DB:              cfg.DB,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		PoolTimeout:     3 * time.Second,
		ConnMaxLifetime: cfg.MaxConnAge,
	})

	if err := Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis at %s: %v", cfg.Addr, err)
	}

	// Initialize metrics
	cacheMetrics = &CacheMetrics{
		startTime: time.Now(),
	}

	log.Printf("Connected to Redis at %s (PoolSize=%d, MinIdleConns=%d, MaxConnAge=%v)",
		cfg.Addr, cfg.PoolSize, cfg.MinIdleConns, cfg.MaxConnAge)
	return nil
}

// Close terminates the Redis connection
func Close() {
	if Client != nil {
		Client.Close()
	}
}

// GetCacheMetrics returns current cache performance metrics
func GetCacheMetrics() *CacheMetrics {
	if cacheMetrics == nil {
		return nil
	}
	cacheMetrics.mu.RLock()
	defer cacheMetrics.mu.RUnlock()

	// Return a copy
	m := *cacheMetrics
	return &m
}

// RecordHit increments the hit counter (thread-safe)
func RecordHit() {
	if cacheMetrics != nil {
		cacheMetrics.mu.Lock()
		cacheMetrics.Hits++
		cacheMetrics.mu.Unlock()
	}
}

// RecordMiss increments the miss counter (thread-safe)
func RecordMiss() {
	if cacheMetrics != nil {
		cacheMetrics.mu.Lock()
		cacheMetrics.Misses++
		cacheMetrics.mu.Unlock()
	}
}

// RecordSet increments the set counter (thread-safe)
func RecordSet() {
	if cacheMetrics != nil {
		cacheMetrics.mu.Lock()
		cacheMetrics.Sets++
		cacheMetrics.mu.Unlock()
	}
}

// RecordDelete increments the delete counter (thread-safe)
func RecordDelete() {
	if cacheMetrics != nil {
		cacheMetrics.mu.Lock()
		cacheMetrics.Deletes++
		cacheMetrics.mu.Unlock()
	}
}

// RecordError increments the error counter (thread-safe)
func RecordError() {
	if cacheMetrics != nil {
		cacheMetrics.mu.Lock()
		cacheMetrics.Errors++
		cacheMetrics.mu.Unlock()
	}
}

// GetHitRate calculates the cache hit rate percentage
func (m *CacheMetrics) GetHitRate() float64 {
	total := m.Hits + m.Misses
	if total == 0 {
		return 0
	}
	return float64(m.Hits) / float64(total) * 100
}

// GetUptime returns how long the cache has been running
func (m *CacheMetrics) GetUptime() time.Duration {
	return time.Since(m.startTime)
}

// WarmCache pre-populates cache with commonly accessed data
func WarmCache(ctx context.Context) error {
	if Client == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	log.Println("Warming cache with commonly accessed data...")

	// Warm up global stats counter if it doesn't exist
	if _, err := Client.Get(ctx, "stats:total_rows").Result(); err == redis.Nil {
		// Initialize if not set
		Client.Set(ctx, "stats:total_rows", "0", 0)
	}

	log.Println("Cache warming complete")
	return nil
}

// InvalidatePattern deletes all keys matching a pattern
func InvalidatePattern(ctx context.Context, pattern string) error {
	if Client == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	var cursor uint64
	for {
		keys, nextCursor, err := Client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %v", err)
		}

		if len(keys) > 0 {
			if err := Client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("failed to delete keys: %v", err)
			}
			RecordDelete()
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}
