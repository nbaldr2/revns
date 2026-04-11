package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

var Session *gocql.Session

// ScyllaConfig holds configurable connection pool settings
type ScyllaConfig struct {
	Hosts          []string
	Keyspace       string
	NumConns       int // Connections per host
	MaxRetries     int
	Timeout        time.Duration
	ConnectTimeout time.Duration
	WriteTimeout   time.Duration
	Consistency    gocql.Consistency
	MaxPending     int // Max pending requests per connection
}

// DefaultScyllaConfig returns optimized default configuration
func DefaultScyllaConfig() ScyllaConfig {
	return ScyllaConfig{
		Hosts:          []string{"127.0.0.1"},
		Keyspace:       "domain_data",
		NumConns:       8,                // Increased from 4 for better throughput
		MaxRetries:     3,                // Retry on transient failures
		Timeout:        10 * time.Second, // Lower timeout for queries (not batches)
		ConnectTimeout: 5 * time.Second,
		WriteTimeout:   30 * time.Second,
		Consistency:    gocql.LocalOne, // LOCAL_ONE for single-DC deployments
		MaxPending:     256,            // Pipeline more requests per connection
	}
}

// Initialize connects to the ScyllaDB cluster with optimized connection pooling
func Initialize(ctx context.Context, hosts []string, keyspace string) error {
	cfg := DefaultScyllaConfig()
	cfg.Keyspace = keyspace
	if len(hosts) > 0 {
		cfg.Hosts = hosts
	}

	// Override with environment variables if set
	if v := os.Getenv("SCYLLA_NUM_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.NumConns = n
		}
	}
	if v := os.Getenv("SCYLLA_MAX_PENDING"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxPending = n
		}
	}
	if v := os.Getenv("SCYLLA_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxRetries = n
		}
	}
	if v := os.Getenv("SCYLLA_CONSISTENCY"); v != "" {
		switch strings.ToUpper(v) {
		case "ONE":
			cfg.Consistency = gocql.One
		case "LOCAL_ONE":
			cfg.Consistency = gocql.LocalOne
		case "QUORUM":
			cfg.Consistency = gocql.Quorum
		}
	}

	return InitializeWithConfig(ctx, cfg)
}

// InitializeWithConfig creates a ScyllaDB session with explicit configuration
func InitializeWithConfig(ctx context.Context, cfg ScyllaConfig) error {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = cfg.Consistency
	cluster.ProtoVersion = 4

	// Connection pool optimization
	cluster.NumConns = cfg.NumConns
	cluster.MaxWaitSchemaAgreement = 60 * time.Second
	cluster.SocketKeepalive = 30 * time.Second
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.WriteTimeout = cfg.WriteTimeout
	cluster.MaxRoutingKeyInfo = 1024 // Cache prepared statement routing

	// Connection pooling with DCAware and token-aware routing
	cluster.PoolConfig = gocql.PoolConfig{
		HostSelectionPolicy: gocql.TokenAwareHostPolicy(
			gocql.DCAwareRoundRobinPolicy("datacenter1"),
			gocql.ShuffleReplicas(),
		),
	}

	// Retry policy for transient failures
	cluster.RetryPolicy = &gocql.SimpleRetryPolicy{
		NumRetries: cfg.MaxRetries,
	}

	// Create session
	var err error
	Session, err = cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to ScyllaDB: %v", err)
	}

	log.Printf("Connected to ScyllaDB cluster at %s (Keyspace: %s)", strings.Join(cfg.Hosts, ","), cfg.Keyspace)
	log.Printf("Connection pool: NumConns=%d, MaxPending=%d, Consistency=%s, MaxRetries=%d",
		cfg.NumConns, cfg.MaxPending, cfg.Consistency, cfg.MaxRetries)
	return nil
}

// Close terminates the ScyllaDB session
func Close() {
	if Session != nil {
		Session.Close()
	}
}
