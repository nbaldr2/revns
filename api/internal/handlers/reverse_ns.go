package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/compression"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/models"
)

var requestGroup singleflight.Group

const (
	defaultLimit = 100
	maxLimit     = 10000 // Hard limit - cannot safely return millions of domains
	cacheTTL     = 5 * time.Minute
	numBuckets   = 100 // Must match ingestion constant
)

// calculateBucket matches the ingester's bucket calculation for consistency
func calculateBucket(domain string) int {
	hash := 0
	for _, c := range domain {
		hash = (hash*31 + int(c)) % 10007
	}
	return hash % numBuckets
}

// GetReverseNS handles GET /api/v1/ns/:nameserver
func GetReverseNS(c *gin.Context) {
	start := time.Now()
	ns := strings.ToLower(c.Param("nameserver"))
	if ns == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nameserver parameter is required"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))
	if limit < 1 || limit > maxLimit {
		limit = defaultLimit
	}

	// Use singleflight to coalesce concurrent requests for the same nameserver
	key := fmt.Sprintf("%s:%d:%d", ns, page, limit)
	result, err, shared := requestGroup.Do(key, func() (interface{}, error) {
		return fetchReverseNS(c.Request.Context(), ns, page, limit)
	})

	if err != nil {
		if isSchemaMissingError(err) {
			c.JSON(http.StatusOK, models.ReverseNSResponse{
				Nameserver:     ns,
				Page:           page,
				Limit:          limit,
				Total:          0,
				Domains:        []string{},
				Cached:         false,
				ResponseTimeMS: time.Since(start).Milliseconds(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := result.(models.ReverseNSResponse)
	response.ResponseTimeMS = time.Since(start).Milliseconds()
	response.Cached = shared // singleflight indicates cache hit/coalescing

	c.JSON(http.StatusOK, response)
}

func fetchReverseNS(ctx context.Context, ns string, page, limit int) (models.ReverseNSResponse, error) {
	response := models.ReverseNSResponse{
		Nameserver: ns,
		Page:       page,
		Limit:      limit,
	}

	// Try Redis cache first (ZSET for paginated results)
	cacheKey := fmt.Sprintf("ns:%s:domains", ns)
	countKey := fmt.Sprintf("ns:%s:count", ns)

	// Get total count from cache or ScyllaDB
	total, err := cache.Client.Get(ctx, countKey).Int64()
	if err == redis.Nil {
		// Not in cache, fetch from ScyllaDB
		total, err = getTotalCount(ctx, ns)
		if err != nil {
			return response, err
		}
		// Cache the count
		cache.Client.Set(ctx, countKey, total, cacheTTL)
	} else if err != nil {
		return response, err
	}

	response.Total = total

	// Calculate offset
	offset := (page - 1) * limit

	// Try to get from Redis ZSET
	domains, err := fetchFromCache(ctx, cacheKey, offset, limit)
	if err == nil && len(domains) > 0 {
		response.Domains = domains
		response.Cached = true
		return response, nil
	}

	// Fall back to ScyllaDB
	domains, err = fetchFromScylla(ctx, ns, offset, limit)
	if err != nil {
		return response, err
	}

	// FIX: If we got empty domains but total > 0, the count cache is stale
	// This happens when data was deleted from ScyllaDB but the Redis count wasn't invalidated
	if len(domains) == 0 && total > 0 {
		// Invalidate the stale count cache
		cache.Client.Del(ctx, countKey)
		// Re-query the actual count from ScyllaDB
		realTotal, countErr := getTotalCount(ctx, ns)
		if countErr == nil {
			response.Total = realTotal
		}
	}

	response.Domains = domains

	// Only populate cache if we have domains
	if len(domains) > 0 {
		// Async populate cache
		go populateCache(ns, offset, limit, domains)
	}

	return response, nil
}

func getTotalCount(ctx context.Context, ns string) (int64, error) {
	var count int64
	legacyQuery := "SELECT COUNT(*) FROM reverse_ns WHERE ns = ?"
	if err := db.Session.Query(legacyQuery, ns).Scan(&count); err != nil {
		if !isUnconfiguredTableError(err, "reverse_ns") {
			return 0, err
		}
	} else if count > 0 {
		return count, nil
	}

	// Fallback to sharded counts for environments where data was ingested
	// into reverse_ns_sharded only.
	shardedCount, err := getShardedTotalCount(ns)
	if err != nil {
		if isUnconfiguredTableError(err, "reverse_ns_sharded") {
			// Both tables absent/empty: treat as no data instead of 500.
			return count, nil
		}
		return 0, err
	}

	return shardedCount, nil
}

func fetchFromCache(ctx context.Context, cacheKey string, offset, limit int) ([]string, error) {
	// For ZSET: Range by score
	result, err := cache.Client.ZRange(ctx, cacheKey, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func fetchFromCompressedCache(ctx context.Context, ns string, offset, limit int) ([]string, error) {
	// Fetch compressed domain blob and decompress
	domainCacheKey := fmt.Sprintf("ns:%s:domains:compressed", ns)
	compressed, err := cache.Client.Get(ctx, domainCacheKey).Bytes()
	if err != nil {
		return nil, err
	}

	if len(compressed) == 0 {
		return nil, nil
	}

	// Decompress domains
	decompressed, err := compression.DecompressDomainBytes(compressed)
	if err != nil {
		return nil, err
	}

	// Parse domains (newline-separated)
	domains := strings.Split(string(decompressed), "\n")

	// Apply pagination
	start := offset
	if start >= len(domains) {
		return []string{}, nil
	}

	end := start + limit
	if end > len(domains) {
		end = len(domains)
	}

	return domains[start:end], nil
}

func fetchFromScylla(ctx context.Context, ns string, offset, limit int) ([]string, error) {
	// Probe sharded table availability once per request. If it's not configured,
	// transparently fall back to legacy table to avoid 500 errors.
	// Try multiple buckets for more robust probing since data distribution varies
	var probeFound bool
	for probeBucket := 0; probeBucket < 10; probeBucket++ {
		probeIter := db.Session.Query(
			"SELECT domain FROM reverse_ns_sharded WHERE ns = ? AND bucket = ? LIMIT 1",
			ns,
			probeBucket,
		).Iter()
		var probeDomain string
		if probeIter.Scan(&probeDomain) {
			probeFound = true
		}
		if err := probeIter.Close(); err != nil {
			if isUnconfiguredTableError(err, "reverse_ns_sharded") {
				legacyDomains, legacyErr := fetchFromLegacyScylla(ctx, ns, offset, limit)
				if legacyErr != nil {
					if isUnconfiguredTableError(legacyErr, "reverse_ns") {
						// Both schemas absent in this environment.
						return []string{}, nil
					}
					return nil, legacyErr
				}
				return legacyDomains, nil
			}
			return nil, err
		}
		if probeFound {
			break
		}
	}

	// FIX: If probe didn't find data in any bucket, fall back to legacy table
	// This handles the case where data exists in reverse_ns but not in reverse_ns_sharded
	if !probeFound {
		legacyDomains, legacyErr := fetchFromLegacyScylla(ctx, ns, offset, limit)
		if legacyErr != nil {
			if isUnconfiguredTableError(legacyErr, "reverse_ns") {
				// Both schemas absent or empty
				return []string{}, nil
			}
			return nil, legacyErr
		}
		return legacyDomains, nil
	}

	// Parallel query across all buckets for the NS
	// This prevents huge partition reads and distributes load
	type result struct {
		domains []string
		err     error
	}

	results := make(chan result, numBuckets)
	var wg sync.WaitGroup

	// Query each bucket in parallel with goroutines
	for bucket := 0; bucket < numBuckets; bucket++ {
		wg.Add(1)
		go func(bucketNum int) {
			defer wg.Done()

			query := "SELECT domain FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
			iter := db.Session.Query(query, ns, bucketNum).Iter()

			bucketDomains := make([]string, 0)
			var domain string

			for iter.Scan(&domain) {
				bucketDomains = append(bucketDomains, domain)
			}

			if err := iter.Close(); err != nil {
				results <- result{err: err}
				return
			}

			results <- result{domains: bucketDomains}
		}(bucket)
	}

	// Close results channel after all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results
	allDomains := make([]string, 0)
	for res := range results {
		if res.err != nil {
			if isUnconfiguredTableError(res.err, "reverse_ns_sharded") {
				legacyDomains, legacyErr := fetchFromLegacyScylla(ctx, ns, offset, limit)
				if legacyErr != nil {
					if isUnconfiguredTableError(legacyErr, "reverse_ns") {
						return []string{}, nil
					}
					return nil, legacyErr
				}
				return legacyDomains, nil
			}
			return nil, res.err
		}
		allDomains = append(allDomains, res.domains...)
	}

	// Sort domains for consistent pagination
	// Note: This is still limited by maxLimit above
	sort.Strings(allDomains)

	// Apply pagination
	start := offset
	if start >= len(allDomains) {
		return []string{}, nil
	}

	end := start + limit
	if end > len(allDomains) {
		end = len(allDomains)
	}

	return allDomains[start:end], nil
}

func fetchFromLegacyScylla(ctx context.Context, ns string, offset, limit int) ([]string, error) {
	query := "SELECT domain FROM reverse_ns WHERE ns = ?"
	iter := db.Session.Query(query, ns).WithContext(ctx).Iter()

	domains := make([]string, 0)
	var domain string

	for iter.Scan(&domain) {
		domains = append(domains, domain)
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	start := offset
	if start >= len(domains) {
		return []string{}, nil
	}

	end := start + limit
	if end > len(domains) {
		end = len(domains)
	}

	return domains[start:end], nil
}

func getShardedTotalCount(ns string) (int64, error) {
	var total int64

	for bucket := 0; bucket < numBuckets; bucket++ {
		var bucketCount int64
		query := "SELECT COUNT(*) FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
		if err := db.Session.Query(query, ns, bucket).Scan(&bucketCount); err != nil {
			return 0, err
		}
		total += bucketCount
	}

	return total, nil
}

func isUnconfiguredTableError(err error, table string) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	tableLower := strings.ToLower(table)

	// Common Cassandra/Scylla variants:
	// - "unconfigured table reverse_ns_sharded"
	// - "unconfigured table domain_data.reverse_ns_sharded"
	if strings.Contains(errMsg, "unconfigured table "+tableLower) {
		return true
	}
	if strings.Contains(errMsg, "unconfigured table") && strings.Contains(errMsg, "."+tableLower) {
		return true
	}
	if strings.Contains(errMsg, "unconfigured table") && strings.Contains(errMsg, tableLower) {
		return true
	}

	// Additional defensive matching for other drivers/versions.
	if strings.Contains(errMsg, "undefined table") && strings.Contains(errMsg, tableLower) {
		return true
	}

	return false
}

func isSchemaMissingError(err error) bool {
	return isUnconfiguredTableError(err, "reverse_ns_sharded") ||
		isUnconfiguredTableError(err, "reverse_ns")
}

// GetAllDomains handles GET /api/v1/ns/:nameserver/all - returns all domains without pagination
func GetAllDomains(c *gin.Context) {
	start := time.Now()
	ns := strings.ToLower(c.Param("nameserver"))
	if ns == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nameserver parameter is required"})
		return
	}

	// Use singleflight to coalesce concurrent requests
	result, err, _ := requestGroup.Do(ns+":all", func() (interface{}, error) {
		return fetchAllDomains(c.Request.Context(), ns)
	})

	if err != nil {
		if isSchemaMissingError(err) {
			c.JSON(http.StatusOK, gin.H{
				"nameserver":       ns,
				"total":            0,
				"domains":          []string{},
				"response_time_ms": time.Since(start).Milliseconds(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	domains := result.([]string)
	c.JSON(http.StatusOK, gin.H{
		"nameserver":       ns,
		"total":            len(domains),
		"domains":          domains,
		"response_time_ms": time.Since(start).Milliseconds(),
	})
}

func fetchAllDomains(ctx context.Context, ns string) ([]string, error) {
	// Prefer legacy table first for backward compatibility, then fall back to
	// sharded storage when legacy table is absent.
	domains, err := fetchAllDomainsFromLegacy(ctx, ns)
	if err == nil {
		return domains, nil
	}
	if !isUnconfiguredTableError(err, "reverse_ns") {
		return nil, err
	}

	shardedDomains, shardedErr := fetchAllDomainsFromSharded(ctx, ns)
	if shardedErr != nil {
		if isUnconfiguredTableError(shardedErr, "reverse_ns_sharded") {
			// Both tables are missing in this environment. Return an empty result
			// instead of surfacing a 500.
			return []string{}, nil
		}
		return nil, shardedErr
	}

	return shardedDomains, nil
}

func fetchAllDomainsFromLegacy(ctx context.Context, ns string) ([]string, error) {
	query := "SELECT domain FROM reverse_ns WHERE ns = ?"
	iter := db.Session.Query(query, ns).WithContext(ctx).Iter()

	domains := make([]string, 0)
	var domain string

	for iter.Scan(&domain) {
		domains = append(domains, domain)
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return domains, nil
}

func fetchAllDomainsFromSharded(ctx context.Context, ns string) ([]string, error) {
	type bucketResult struct {
		domains []string
		err     error
	}

	results := make(chan bucketResult, numBuckets)
	var wg sync.WaitGroup

	for bucket := 0; bucket < numBuckets; bucket++ {
		wg.Add(1)
		go func(bucketNum int) {
			defer wg.Done()

			query := "SELECT domain FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
			iter := db.Session.Query(query, ns, bucketNum).WithContext(ctx).Iter()

			bucketDomains := make([]string, 0)
			var domain string
			for iter.Scan(&domain) {
				bucketDomains = append(bucketDomains, domain)
			}

			if err := iter.Close(); err != nil {
				results <- bucketResult{err: err}
				return
			}

			results <- bucketResult{domains: bucketDomains}
		}(bucket)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	allDomains := make([]string, 0)
	for res := range results {
		if res.err != nil {
			return nil, res.err
		}
		allDomains = append(allDomains, res.domains...)
	}

	sort.Strings(allDomains)
	return allDomains, nil
}

func populateCache(ns string, offset, limit int, domains []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cacheKey := fmt.Sprintf("ns:%s:domains", ns)
	pipe := cache.Client.Pipeline()

	for i, domain := range domains {
		score := float64(offset + i)
		pipe.ZAdd(ctx, cacheKey, redis.Z{Score: score, Member: domain})
	}

	pipe.Expire(ctx, cacheKey, cacheTTL)
	pipe.Exec(ctx)
}
