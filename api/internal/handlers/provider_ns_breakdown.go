package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/middleware"
	"github.com/soufianerochdi/revns-api/internal/models"
)

var providerNSGroup singleflight.Group

const providerNSCacheTTL = 30 * time.Minute // Increased for millions of records

// GetProviderNSBreakdown handles GET /api/v1/hosting-providers/:provider/ns
func GetProviderNSBreakdown(c *gin.Context) {
	start := time.Now()
	provider := strings.TrimSpace(c.Param("provider"))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider parameter is required"})
		return
	}

	// Add timeout to prevent hanging requests
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	cacheProvider := strings.ToLower(provider)
	result, err, shared := providerNSGroup.Do("ns-breakdown:"+cacheProvider, func() (interface{}, error) {
		return fetchProviderNSBreakdownFromTable(ctx, provider, cacheProvider)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	breakdown := result.(models.ProviderNSBreakdownResponse)
	breakdown.ResponseTimeMS = time.Since(start).Milliseconds()
	breakdown.Cached = shared

	c.JSON(http.StatusOK, breakdown)
}

// fetchProviderNSBreakdownFromTable reads from provider_ns table with pre-computed counts
// ULTRA-OPTIMIZED: Single query gets all NS + counts, no extra lookups needed
func fetchProviderNSBreakdownFromTable(ctx context.Context, provider, cacheProvider string) (models.ProviderNSBreakdownResponse, error) {
	response := models.ProviderNSBreakdownResponse{
		Provider: provider,
		NSCounts: make([]models.NSCount, 0),
	}

	// Try cache first
	cacheKey := fmt.Sprintf("hosting-providers:ns-breakdown:%s", cacheProvider)
	cachedData, err := cache.Client.Get(ctx, cacheKey).Result()
	if err == nil {
		var cachedResponse models.ProviderNSBreakdownResponse
		if unmarshalErr := json.Unmarshal([]byte(cachedData), &cachedResponse); unmarshalErr == nil {
			cachedResponse.Cached = true
			return cachedResponse, nil
		}
	} else if err != redis.Nil {
		return response, err
	}

	// ULTRA-OPTIMIZED: Get NS + domain_count in ONE query from provider_ns table
	// This uses pre-computed counts from ingestion time - no need for extra lookups
	query := "SELECT ns, domain_count FROM provider_ns WHERE provider = ?"
	iter := db.Session.Query(query, provider).WithContext(ctx).Iter()

	var ns string
	var domainCount int64
	for iter.Scan(&ns, &domainCount) {
		// Skip if domain_count is missing or 0, try to get from ns_stats as fallback
		if domainCount == 0 {
			domainCount = getNSCountFromStats(ctx, ns)
		}
		response.NSCounts = append(response.NSCounts, models.NSCount{
			Nameserver: ns,
			Count:      domainCount,
		})
		response.TotalDomains += domainCount
	}

	if err := iter.Close(); err != nil {
		return response, err
	}

	// Sort by count (descending)
	sort.Slice(response.NSCounts, func(i, j int) bool {
		return response.NSCounts[i].Count > response.NSCounts[j].Count
	})

	response.TotalNS = int64(len(response.NSCounts))

	// Cache the results
	if data, marshalErr := json.Marshal(response); marshalErr == nil {
		cache.Client.Set(ctx, cacheKey, string(data), providerNSCacheTTL)
	}

	return response, nil
}

// getNSCountsConcurrent fetches counts for all NS in parallel using ns_stats table
// Falls back to concurrent sharded queries only if ns_stats is missing data
func getNSCountsConcurrent(ctx context.Context, nsList []string) []models.NSCount {
	if len(nsList) == 0 {
		return []models.NSCount{}
	}

	results := make([]models.NSCount, len(nsList))
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use worker pool to limit concurrent queries (20 workers max)
	sem := make(chan struct{}, 20)

	for i, ns := range nsList {
		wg.Add(1)
		go func(index int, nameserver string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			count := getNSCountFast(ctx, nameserver)

			mu.Lock()
			results[index] = models.NSCount{
				Nameserver: nameserver,
				Count:      count,
			}
			mu.Unlock()

			// Record DB query duration for metrics
			middleware.RecordDBQuery("ns_count_lookup", time.Millisecond*10)
		}(i, ns)
	}

	wg.Wait()
	return results
}

// getNSCountFast uses ns_stats table first (O(1)), falls back to concurrent sharded query
func getNSCountFast(ctx context.Context, ns string) int64 {
	// PRIMARY: Check ns_stats table (pre-computed count)
	var cachedCount int64
	query := "SELECT domain_count FROM ns_stats WHERE ns = ?"
	err := db.Session.Query(query, ns).WithContext(ctx).Scan(&cachedCount)
	if err == nil && cachedCount > 0 {
		return cachedCount
	}

	// FALLBACK 1: Try legacy reverse_ns table
	var legacyCount int64
	query = "SELECT COUNT(*) FROM reverse_ns WHERE ns = ?"
	err = db.Session.Query(query, ns).WithContext(ctx).Scan(&legacyCount)
	if err == nil && legacyCount > 0 {
		return legacyCount
	}

	// FALLBACK 2: Query all 100 buckets concurrently with worker pool
	var total int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10) // 10 concurrent bucket queries max

	for bucket := 0; bucket < 100; bucket++ {
		wg.Add(1)
		go func(b int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			var bucketCount int64
			query := "SELECT COUNT(*) FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
			err := db.Session.Query(query, ns, b).WithContext(ctx).Scan(&bucketCount)
			if err == nil {
				mu.Lock()
				total += bucketCount
				mu.Unlock()
			}
		}(bucket)
	}

	wg.Wait()
	return total
}

// getNSCountFromStats is a lightweight fallback that only checks ns_stats table
func getNSCountFromStats(ctx context.Context, ns string) int64 {
	var count int64
	query := "SELECT domain_count FROM ns_stats WHERE ns = ?"
	err := db.Session.Query(query, ns).WithContext(ctx).Scan(&count)
	if err == nil {
		return count
	}
	return 0
}
