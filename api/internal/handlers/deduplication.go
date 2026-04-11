package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
)

// DeduplicationResult contains the result of deduplication
type DeduplicationResult struct {
	DuplicatesRemoved int64  `json:"duplicates_removed"`
	TotalDomains      int64  `json:"total_domains"`
	TimeTaken         string `json:"time_taken"`
	Message           string `json:"message"`
}

// CleanDuplicates removes duplicate domain entries from the database
// Uses application-level deduplication with batch processing for ScyllaDB compatibility
func CleanDuplicates(c *gin.Context) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	// Get total count before
	totalBefore := countTotalRecords()

	// Step 1: Deduplicate legacy table
	legacyRemoved := cleanLegacyTableFast(ctx)

	// Step 2: Deduplicate sharded table
	shardedRemoved := cleanShardedTableFast(ctx)

	totalRemoved := legacyRemoved + shardedRemoved

	// Invalidate all related cache entries
	invalidateProviderCache(ctx)

	elapsed := time.Since(start)

	c.JSON(http.StatusOK, DeduplicationResult{
		DuplicatesRemoved: totalRemoved,
		TotalDomains:      totalBefore - totalRemoved,
		TimeTaken:         elapsed.String(),
		Message:           "Deduplication completed successfully",
	})
}

// cleanLegacyTableFast uses application-level deduplication (ScyllaDB-compatible)
func cleanLegacyTableFast(ctx context.Context) int64 {
	var totalRemoved int64

	// Use ALLOW FILTERING to find potential duplicates
	// Since ns is the partition key, we need to scan and dedupe within each partition
	iter := db.Session.Query(`
		SELECT ns, domain, rank FROM reverse_ns LIMIT 1000000
	`).Iter()

	// Track seen (ns, domain) pairs
	seen := make(map[string]bool)
	toDelete := make([]string, 0)
	var ns, domain string
	var rank int

	for iter.Scan(&ns, &domain, &rank) {
		key := ns + "|" + domain
		if seen[key] {
			// Duplicate found - mark for deletion
			toDelete = append(toDelete, ns, domain)
		} else {
			seen[key] = true
		}
	}

	if err := iter.Close(); err != nil {
		log.Printf("Error scanning legacy table: %v", err)
	}

	// Delete duplicates in batches
	for i := 0; i < len(toDelete); i += 2 {
		nsVal := toDelete[i]
		domainVal := toDelete[i+1]
		err := db.Session.Query(
			"DELETE FROM reverse_ns WHERE ns = ? AND domain = ?",
			nsVal, domainVal,
		).Exec()
		if err == nil {
			totalRemoved++
		}
	}

	return totalRemoved
}

// cleanShardedTableFast uses application-level deduplication for sharded table
func cleanShardedTableFast(ctx context.Context) int64 {
	var totalRemoved int64
	const numBuckets = 100

	for bucket := 0; bucket < numBuckets; bucket++ {
		removed := cleanShardedBucketFast(ctx, bucket)
		totalRemoved += removed
	}

	return totalRemoved
}

// cleanShardedBucketFast cleans duplicates from a single bucket
func cleanShardedBucketFast(ctx context.Context, bucket int) int64 {
	var removed int64

	// Scan all records in this bucket
	iter := db.Session.Query(`
		SELECT ns, domain FROM reverse_ns_sharded WHERE bucket = ?
	`, bucket).Iter()

	// Track seen (ns, domain) pairs
	seen := make(map[string]bool)
	toDelete := make([]string, 0)
	var ns, domain string

	for iter.Scan(&ns, &domain) {
		key := ns + "|" + domain
		if seen[key] {
			toDelete = append(toDelete, ns, domain)
		} else {
			seen[key] = true
		}
	}

	if err := iter.Close(); err != nil {
		log.Printf("Error scanning bucket %d: %v", bucket, err)
	}

	// Delete duplicates
	for i := 0; i < len(toDelete); i += 2 {
		nsVal := toDelete[i]
		domainVal := toDelete[i+1]
		err := db.Session.Query(
			"DELETE FROM reverse_ns_sharded WHERE bucket = ? AND ns = ? AND domain = ?",
			bucket, nsVal, domainVal,
		).Exec()
		if err == nil {
			removed++
		}
	}

	return removed
}

// countTotalRecords counts all records across all tables
func countTotalRecords() int64 {
	var total int64

	// Count legacy table
	var legacyCount int64
	err := db.Session.Query("SELECT COUNT(*) FROM reverse_ns").Scan(&legacyCount)
	if err == nil {
		total += legacyCount
	}

	// Count sharded table (sample buckets and estimate)
	var sampleCount int64
	for bucket := 0; bucket < 10; bucket++ {
		var bucketCount int64
		err := db.Session.Query(
			"SELECT COUNT(*) FROM reverse_ns_sharded WHERE bucket = ?",
			bucket,
		).Scan(&bucketCount)
		if err == nil {
			sampleCount += bucketCount
		}
	}

	// Estimate total from sample
	if sampleCount > 0 {
		total += sampleCount * 10
	}

	return total
}

// invalidateProviderCache clears all provider-related cache entries
func invalidateProviderCache(ctx context.Context) {
	cacheKeys := []string{
		"hosting-providers:all",
		"hosting-providers:top10",
		"global-stats",
		"total-domains",
	}

	for _, key := range cacheKeys {
		cache.Client.Del(ctx, key)
	}
}

// GetDuplicateStats returns statistics about duplicates
func GetDuplicateStats(c *gin.Context) {
	stats := make(map[string]interface{})

	// Count duplicates in legacy table using application-level scan
	legacyDups := countDuplicatesInLegacy()
	stats["legacy_table_duplicates"] = legacyDups

	// Count duplicates in sharded table (sample)
	shardedDups := countDuplicatesInShardedSample()
	stats["sharded_table_bucket_sample"] = shardedDups
	stats["sharded_table_estimated"] = shardedDups * 10

	c.JSON(http.StatusOK, stats)
}

// countDuplicatesInLegacy counts duplicates in legacy table
func countDuplicatesInLegacy() int64 {
	var duplicates int64

	iter := db.Session.Query(`
		SELECT ns, domain FROM reverse_ns LIMIT 1000000
	`).Iter()

	seen := make(map[string]bool)
	var ns, domain string

	for iter.Scan(&ns, &domain) {
		key := ns + "|" + domain
		if seen[key] {
			duplicates++
		} else {
			seen[key] = true
		}
	}

	iter.Close()
	return duplicates
}

// countDuplicatesInShardedSample counts duplicates in first 10 buckets
func countDuplicatesInShardedSample() int64 {
	var duplicates int64

	for bucket := 0; bucket < 10; bucket++ {
		iter := db.Session.Query(`
			SELECT ns, domain FROM reverse_ns_sharded WHERE bucket = ?
		`, bucket).Iter()

		seen := make(map[string]bool)
		var ns, domain string

		for iter.Scan(&ns, &domain) {
			key := ns + "|" + domain
			if seen[key] {
				duplicates++
			} else {
				seen[key] = true
			}
		}

		iter.Close()
	}

	return duplicates
}

// DedupStatsResponse for the API
type DedupStatsResponse struct {
	LegacyTableDuplicates  int64 `json:"legacy_table_duplicates"`
	ShardedTableDuplicates int64 `json:"sharded_table_duplicates"`
	EstimatedTotal         int64 `json:"estimated_total"`
}
