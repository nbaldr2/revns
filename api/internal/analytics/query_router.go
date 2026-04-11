package analytics

import (
	"context"
	"fmt"
	"log"
)

// QueryRouter decides which database to use based on query characteristics
// Rule of thumb: <10,000 rows → ScyllaDB, >10,000 rows → ClickHouse
type QueryRouter struct {
	largeNSThreshold int64
}

// NewQueryRouter creates a new query router with configurable threshold
func NewQueryRouter(threshold int64) *QueryRouter {
	return &QueryRouter{
		largeNSThreshold: threshold,
	}
}

// ShouldUseClickHouse determines if a nameserver query should use ClickHouse
func (qr *QueryRouter) ShouldUseClickHouse(ctx context.Context, ns string) (bool, error) {
	if !IsConnected() {
		return false, nil // Fall back to ScyllaDB
	}

	// Quick count check via ClickHouse
	var count uint64
	err := GetConnection().QueryRow(ctx, `
		SELECT count() FROM reverse_ns WHERE ns = ?
	`, ns).Scan(&count)

	if err != nil {
		log.Printf("Warning: Failed to check NS count in ClickHouse: %v", err)
		return false, nil // Fall back to ScyllaDB
	}

	return int64(count) > qr.largeNSThreshold, nil
}

// GetNameserverDomainCount returns the domain count for a nameserver
func (qr *QueryRouter) GetNameserverDomainCount(ctx context.Context, ns string) (int64, error) {
	var count uint64
	err := GetConnection().QueryRow(ctx, `
		SELECT count() FROM reverse_ns WHERE ns = ?
	`, ns).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get domain count: %w", err)
	}
	return int64(count), nil
}

// Default threshold constants
const (
	// LargeNSThreshold is the number of domains above which a nameserver is considered "large"
	LargeNSThreshold = 10000
)

// Default router instance
var defaultRouter = NewQueryRouter(LargeNSThreshold)

// ShouldUseClickHouseForNS is a convenience function using the default router
func ShouldUseClickHouseForNS(ctx context.Context, ns string) (bool, error) {
	return defaultRouter.ShouldUseClickHouse(ctx, ns)
}

// GetDomainCountForNS returns the domain count for a nameserver using the default router
func GetDomainCountForNS(ctx context.Context, ns string) (int64, error) {
	return defaultRouter.GetNameserverDomainCount(ctx, ns)
}
