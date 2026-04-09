package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/handlers"
	"github.com/soufianerochdi/revns-api/internal/middleware"
)

func main() {
	ctx := context.Background()

	// Initialize ScyllaDB
	scyllaHosts := []string{"127.0.0.1"}
	if os.Getenv("SCYLLA_HOSTS") != "" {
		scyllaHosts = []string{os.Getenv("SCYLLA_HOSTS")}
	}

	if err := db.Initialize(ctx, scyllaHosts, "domain_data"); err != nil {
		panic(err)
	}
	defer db.Close()

	// Initialize Redis
	redisAddr := "127.0.0.1:6379"
	if os.Getenv("REDIS_ADDR") != "" {
		redisAddr = os.Getenv("REDIS_ADDR")
	}
	if err := cache.Initialize(ctx, redisAddr); err != nil {
		panic(err)
	}
	defer cache.Close()

	// Initialize logger
	logger, err := middleware.NewLogger()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	// Initialize rate limiter: 100 requests per second with burst of 200
	rateLimiter := middleware.NewRateLimiter(rate.Limit(100), 200)
	rateLimiter.Cleanup(10 * time.Minute)

	// Setup Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(logger.Middleware())
	r.Use(middleware.PrometheusMiddleware())

	// Health checks
	r.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/health/ready", func(c *gin.Context) {
		// Check ScyllaDB
		if db.Session == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "reason": "database"})
			return
		}
		// Check Redis
		if cache.Client == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "reason": "cache"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Metrics endpoint
	r.GET("/metrics", middleware.MetricsHandler())

	// API v1 routes
	v1 := r.Group("/api/v1")
	v1.Use(rateLimiter.Middleware())
	{
		v1.GET("/ns/:nameserver", handlers.GetReverseNS)
		v1.GET("/ns/:nameserver/all", handlers.GetAllDomains)
		v1.GET("/stats", handlers.GetGlobalStats)
		v1.GET("/hosting-providers/top", handlers.GetTopHostingProviders)
		v1.GET("/hosting-providers", handlers.GetAllHostingProviders)
		v1.GET("/hosting-providers/:provider/ns", handlers.GetProviderNSBreakdown)
		v1.POST("/upload", handlers.UploadCSV)
		v1.GET("/upload/status", handlers.GetUploadStatus)
		v1.GET("/upload/errors", handlers.GetUploadErrors)
	}

	// Server configuration
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		panic(err)
	}
}
