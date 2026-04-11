package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiterConfig holds configurable rate limiting settings
type RateLimiterConfig struct {
	// Global rate limit
	GlobalRate  rate.Limit
	GlobalBurst int
	// Per-IP rate limit
	IPRate  rate.Limit
	IPBurst int
	// TTL for per-IP limiters
	IPTTL time.Duration
	// Max concurrent requests
	MaxConcurrent int
	// Upload-specific limits
	UploadMaxSize       int64 // Max upload size in bytes
	UploadRateLimit     rate.Limit
	UploadMaxConcurrent int
}

// DefaultRateLimiterConfig returns optimized default configuration
func DefaultRateLimiterConfig() RateLimiterConfig {
	cfg := RateLimiterConfig{
		GlobalRate:          1000,              // 1000 requests/sec global
		GlobalBurst:         2000,              // Allow bursts up to 2000
		IPRate:              100,               // 100 requests/sec per IP
		IPBurst:             200,               // Allow bursts up to 200 per IP
		IPTTL:               10 * time.Minute,  // Clean up unused IP limiters
		MaxConcurrent:       500,               // Max concurrent requests
		UploadMaxSize:       250 * 1024 * 1024, // 250MB max upload
		UploadRateLimit:     10,                // 10 uploads/sec per IP
		UploadMaxConcurrent: 5,                 // Max 5 concurrent uploads
	}

	// Override with environment variables if set
	if v := os.Getenv("RATE_LIMIT_GLOBAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.GlobalRate = rate.Limit(n)
		}
	}
	if v := os.Getenv("RATE_LIMIT_IP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.IPRate = rate.Limit(n)
		}
	}
	if v := os.Getenv("RATE_LIMIT_BURST_IP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.IPBurst = n
		}
	}
	if v := os.Getenv("RATE_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrent = n
		}
	}
	if v := os.Getenv("UPLOAD_MAX_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.UploadMaxSize = int64(n) * 1024 * 1024
		}
	}
	if v := os.Getenv("UPLOAD_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.UploadRateLimit = rate.Limit(n)
		}
	}
	if v := os.Getenv("UPLOAD_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.UploadMaxConcurrent = n
		}
	}

	return cfg
}

// IPRateLimiter tracks per-IP rate limiting with automatic cleanup
type IPRateLimiter struct {
	limiters map[string]*ipLimiterEntry
	mu       sync.RWMutex
	cfg      RateLimiterConfig
}

type ipLimiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// NewIPRateLimiter creates a new rate limiter middleware
func NewIPRateLimiter(cfg RateLimiterConfig) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*ipLimiterEntry),
		cfg:      cfg,
	}
}

// getOrCreateLimiter returns existing limiter for the IP or creates new one
func (rl *IPRateLimiter) getOrCreateLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	entry, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	// Create new limiter with write lock
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	entry, exists = rl.limiters[ip]
	if exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(rl.cfg.IPRate, rl.cfg.IPBurst)
	rl.limiters[ip] = &ipLimiterEntry{
		limiter:    limiter,
		lastAccess: time.Now(),
	}
	return limiter
}

// Cleanup removes expired limiters to prevent memory growth
func (rl *IPRateLimiter) Cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for ip, entry := range rl.limiters {
				if now.Sub(entry.lastAccess) > rl.cfg.IPTTL {
					delete(rl.limiters, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
}

// Middleware returns the per-IP rate limiting Gin middleware
func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := rl.getOrCreateLimiter(ip)

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": fmt.Sprintf("%.1f", limiter.Reserve().DelayFrom(time.Now()).Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ConcurrentLimiter limits the number of concurrent requests
type ConcurrentLimiter struct {
	semaphore chan struct{}
}

// NewConcurrentLimiter creates a new concurrent request limiter
func NewConcurrentLimiter(max int) *ConcurrentLimiter {
	return &ConcurrentLimiter{
		semaphore: make(chan struct{}, max),
	}
}

// Middleware returns the concurrent request limiting Gin middleware
func (cl *ConcurrentLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		select {
		case cl.semaphore <- struct{}{}:
			defer func() { <-cl.semaphore }()
			c.Next()
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "server is busy, please try again later",
			})
			c.Abort()
		}
	}
}
