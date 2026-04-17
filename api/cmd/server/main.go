package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/soufianerochdi/revns-api/internal/analytics"
	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/circuitbreaker"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/handlers"
	"github.com/soufianerochdi/revns-api/internal/middleware"
)

// Global circuit breakers
var (
	scyllaCB     *circuitbreaker.CircuitBreaker
	redisCB      *circuitbreaker.CircuitBreaker
	clickHouseCB *circuitbreaker.CircuitBreaker
)

// Graceful shutdown tracking
var (
	isShuttingDown atomic.Bool
	shutdownWg     sync.WaitGroup
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Initialize circuit breakers
	scyllaCB = circuitbreaker.New(circuitbreaker.Config{
		MaxFailures:   5,
		ResetTimeout:  30 * time.Second,
		Timeout:       10 * time.Second,
		MaxConcurrent: 500,
	})
	scyllaCB.OnStateChange = func(old, newState string) {
		log.Printf("ScyllaDB circuit breaker: %s -> %s", old, newState)
	}

	redisCB = circuitbreaker.New(circuitbreaker.Config{
		MaxFailures:   3,
		ResetTimeout:  10 * time.Second,
		Timeout:       3 * time.Second,
		MaxConcurrent: 1000,
	})
	redisCB.OnStateChange = func(old, newState string) {
		log.Printf("Redis circuit breaker: %s -> %s", old, newState)
	}

	// Initialize ScyllaDB
	scyllaHosts := []string{"127.0.0.1"}
	if os.Getenv("SCYLLA_HOSTS") != "" {
		scyllaHosts = []string{os.Getenv("SCYLLA_HOSTS")}
	}

	if err := db.Initialize(ctx, scyllaHosts, "domain_data"); err != nil {
		log.Printf("WARNING: ScyllaDB initialization failed: %v", err)
		scyllaCB.Open()
	} else {
		defer db.Close()
	}

	// Initialize Redis
	redisAddr := "127.0.0.1:6379"
	if os.Getenv("REDIS_ADDR") != "" {
		redisAddr = os.Getenv("REDIS_ADDR")
	}
	if err := cache.Initialize(ctx, redisAddr); err != nil {
		log.Printf("WARNING: Redis initialization failed: %v", err)
		redisCB.Open()
	} else {
		defer cache.Close()
		// Warm up cache
		if err := cache.WarmCache(ctx); err != nil {
			log.Printf("WARNING: Cache warming failed: %v", err)
		}
	}

	// Initialize ClickHouse (optional - for analytics)
	clickHouseCB = circuitbreaker.New(circuitbreaker.Config{
		MaxFailures:   5,
		ResetTimeout:  30 * time.Second,
		Timeout:       10 * time.Second,
		MaxConcurrent: 100,
	})
	clickHouseCB.OnStateChange = func(old, newState string) {
		log.Printf("ClickHouse circuit breaker: %s -> %s", old, newState)
	}

	clickHouseAddr := "127.0.0.1:9000"
	if os.Getenv("CLICKHOUSE_ADDR") != "" {
		clickHouseAddr = os.Getenv("CLICKHOUSE_ADDR")
	}
	if os.Getenv("USE_CLICKHOUSE_ANALYTICS") == "true" {
		if err := initClickHouse(ctx, clickHouseAddr); err != nil {
			log.Printf("WARNING: ClickHouse initialization failed: %v", err)
			clickHouseCB.Open()
		}
	}

	// Initialize logger
	logger, err := middleware.NewLogger()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Initialize rate limiting middleware
	rateCfg := middleware.DefaultRateLimiterConfig()
	ipRateLimiter := middleware.NewIPRateLimiter(rateCfg)
	ipRateLimiter.Cleanup()

	// Concurrent request limiter
	concurrentLimiter := middleware.NewConcurrentLimiter(rateCfg.MaxConcurrent)

	// Setup Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(logger.Middleware())
	router.Use(middleware.PrometheusMiddleware())
	router.Use(concurrentLimiter.Middleware())

	// Health checks with actual connectivity verification
	router.GET("/health/live", func(c *gin.Context) {
		// Liveness: just return OK if the process is running
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	router.GET("/health/ready", func(c *gin.Context) {
		// Readiness: check all dependencies
		status := gin.H{
			"status": "ok",
			"checks": gin.H{},
		}
		healthy := true

		// Check ScyllaDB with circuit breaker
		dbStatus := checkDatabaseHealth(c.Request.Context())
		status["checks"].(gin.H)["database"] = dbStatus
		if dbStatus["status"] != "ok" {
			healthy = false
		}

		// Check Redis with circuit breaker
		redisStatus := checkRedisHealth(c.Request.Context())
		status["checks"].(gin.H)["cache"] = redisStatus
		if redisStatus["status"] != "ok" {
			healthy = false
		}

		// Add circuit breaker metrics
		status["circuit_breakers"] = gin.H{
			"database": scyllaCB.GetMetrics(),
			"cache":    redisCB.GetMetrics(),
		}

		if healthy {
			c.JSON(http.StatusOK, status)
		} else {
			c.JSON(http.StatusServiceUnavailable, status)
		}
	})

	// Circuit breaker status endpoint
	router.GET("/health/circuit-breakers", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"database": scyllaCB.GetMetrics(),
			"cache":    redisCB.GetMetrics(),
		})
	})

	// Metrics endpoint
	router.GET("/metrics", middleware.MetricsHandler())

	// API v1 routes
	v1 := router.Group("/api/v1")
	v1.Use(ipRateLimiter.Middleware())
	{
		v1.GET("/ns/:nameserver", handlers.GetReverseNS)
		v1.GET("/ns/:nameserver/all", handlers.GetAllDomains)
		v1.GET("/stats", handlers.GetGlobalStats)
		v1.GET("/hosting-providers/top", handlers.GetTopHostingProviders)
		v1.GET("/hosting-providers", handlers.GetAllHostingProviders)
		v1.GET("/hosting-providers/:provider/ns", handlers.GetProviderNSBreakdown)
		v1.GET("/provider-search", handlers.GetProviderDomainSearch)
		v1.GET("/provider-search.csv", handlers.GetProviderDomainSearchCSV)
		v1.POST("/upload", handlers.UploadCSV)
		v1.GET("/upload/status", handlers.GetUploadStatus)
		v1.GET("/upload/errors", handlers.GetUploadErrors)
		// Deduplication endpoints
		v1.POST("/deduplicate", handlers.CleanDuplicates)
		v1.GET("/duplicates/stats", handlers.GetDuplicateStats)
	}

	// Server configuration with proper timeouts
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("Starting server on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal: %v, starting graceful shutdown...", sig)

	// Mark server as shutting down (rejects new requests)
	isShuttingDown.Store(true)

	// Graceful shutdown with drain period
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Close idle connections immediately
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
		// Force shutdown if graceful fails
		if err := srv.Close(); err != nil {
			log.Printf("Server force close error: %v", err)
		}
	}

	// Wait for in-flight requests to complete (with timeout)
	done := make(chan struct{})
	go func() {
		shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All in-flight requests completed")
	case <-time.After(10 * time.Second):
		log.Println("Shutdown timeout reached, forcing exit")
	}

	log.Println("Server shutdown complete")
}

// checkDatabaseHealth verifies ScyllaDB connectivity
func checkDatabaseHealth(ctx context.Context) map[string]interface{} {
	// Check circuit breaker state first
	if !scyllaCB.IsReady() {
		return map[string]interface{}{
			"status":  "unhealthy",
			"reason":  "circuit breaker is open",
			"metrics": scyllaCB.GetMetrics(),
		}
	}

	result := make(chan map[string]interface{}, 1)

	go func() {
		if db.Session == nil {
			result <- map[string]interface{}{
				"status": "unhealthy",
				"reason": "session is nil",
			}
			return
		}

		// Execute a simple query to verify connectivity
		err := scyllaCB.Execute(ctx, func() error {
			var key string
			// Use a fast metadata query instead of table scan
			return db.Session.Query("SELECT key FROM system.local").Scan(&key)
		})

		if err != nil {
			result <- map[string]interface{}{
				"status":  "unhealthy",
				"reason":  fmt.Sprintf("query failed: %v", err),
				"metrics": scyllaCB.GetMetrics(),
			}
		} else {
			result <- map[string]interface{}{
				"status":  "ok",
				"latency": "healthy",
				"metrics": scyllaCB.GetMetrics(),
			}
		}
	}()

	select {
	case res := <-result:
		return res
	case <-time.After(5 * time.Second):
		return map[string]interface{}{
			"status": "unhealthy",
			"reason": "check timed out",
		}
	}
}

// checkRedisHealth verifies Redis connectivity
func checkRedisHealth(ctx context.Context) map[string]interface{} {
	// Check circuit breaker state first
	if !redisCB.IsReady() {
		return map[string]interface{}{
			"status":  "unhealthy",
			"reason":  "circuit breaker is open",
			"metrics": redisCB.GetMetrics(),
		}
	}

	result := make(chan map[string]interface{}, 1)

	go func() {
		if cache.Client == nil {
			result <- map[string]interface{}{
				"status": "unhealthy",
				"reason": "client is nil",
			}
			return
		}

		// Execute a simple ping to verify connectivity
		err := redisCB.Execute(ctx, func() error {
			return cache.Client.Ping(ctx).Err()
		})

		if err != nil {
			result <- map[string]interface{}{
				"status":  "unhealthy",
				"reason":  fmt.Sprintf("ping failed: %v", err),
				"metrics": redisCB.GetMetrics(),
			}
		} else {
			result <- map[string]interface{}{
				"status":  "ok",
				"latency": "healthy",
				"metrics": redisCB.GetMetrics(),
			}
		}
	}()

	select {
	case res := <-result:
		return res
	case <-time.After(3 * time.Second):
		return map[string]interface{}{
			"status": "unhealthy",
			"reason": "check timed out",
		}
	}
}

// initClickHouse initializes the ClickHouse analytics connection
func initClickHouse(ctx context.Context, addr string) error {
	if err := analytics.InitializeClickHouse(ctx, addr); err != nil {
		return fmt.Errorf("failed to initialize ClickHouse: %w", err)
	}
	log.Printf("ClickHouse analytics connected at %s", addr)
	return nil
}

// checkClickHouseHealth verifies ClickHouse connectivity
func checkClickHouseHealth(ctx context.Context) map[string]interface{} {
	if !clickHouseCB.IsReady() {
		return map[string]interface{}{
			"status":  "unhealthy",
			"reason":  "circuit breaker is open",
			"metrics": clickHouseCB.GetMetrics(),
		}
	}

	result := make(chan map[string]interface{}, 1)

	go func() {
		if !analytics.IsConnected() {
			result <- map[string]interface{}{
				"status": "unhealthy",
				"reason": "not connected",
			}
			return
		}

		err := clickHouseCB.Execute(ctx, func() error {
			return analytics.Ping(ctx)
		})

		if err != nil {
			result <- map[string]interface{}{
				"status":  "unhealthy",
				"reason":  fmt.Sprintf("ping failed: %v", err),
				"metrics": clickHouseCB.GetMetrics(),
			}
		} else {
			result <- map[string]interface{}{
				"status":  "ok",
				"latency": "healthy",
				"metrics": clickHouseCB.GetMetrics(),
			}
		}
	}()

	select {
	case res := <-result:
		return res
	case <-time.After(5 * time.Second):
		return map[string]interface{}{
			"status": "unhealthy",
			"reason": "check timed out",
		}
	}
}
