package db

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

var Session *gocql.Session

// Initialize connects to the ScyllaDB cluster
func Initialize(ctx context.Context, hosts []string, keyspace string) error {
	// Configure cluster with optimizations for bulk writes
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.One  // Use ONE for faster writes (acceptable for bulk inserts)
	cluster.ProtoVersion = 4
	
	// Performance optimizations for large data uploads
	cluster.NumConns = 4                           // More connections per host
	cluster.SocketKeepalive = 30 * time.Second     // Keep connections alive
	cluster.Timeout = 30 * time.Second             // Increased timeout for large batches
	cluster.ConnectTimeout = 10 * time.Second      // Connection timeout
	cluster.WriteTimeout = 30 * time.Second        // Write timeout for batches
	cluster.MaxWaitSchemaAgreement = 60 * time.Second
	
	// Connection pooling
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())

	// Create session
	var err error
	Session, err = cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to ScyllaDB: %v", err)
	}

	log.Printf("Connected to ScyllaDB cluster at %s (Keyspace: %s)", strings.Join(hosts, ","), keyspace)
	log.Printf("Connection settings: Consistency=ONE, NumConns=4, Timeout=30s (optimized for bulk inserts)")
	return nil
}

// Close terminates the ScyllaDB session
func Close() {
	if Session != nil {
		Session.Close()
	}
}
