package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/models"
)

var providerNSGroup singleflight.Group

const providerNSCacheTTL = 10 * time.Minute

// GetProviderNSBreakdown handles GET /api/v1/hosting-providers/:provider/ns
func GetProviderNSBreakdown(c *gin.Context) {
	start := time.Now()
	provider := strings.TrimSpace(c.Param("provider"))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider parameter is required"})
		return
	}

	cacheProvider := strings.ToLower(provider)
	result, err, shared := providerNSGroup.Do("ns-breakdown:"+cacheProvider, func() (interface{}, error) {
		return fetchProviderNSBreakdownFromTable(c.Request.Context(), provider, cacheProvider)
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

// fetchProviderNSBreakdownFromTable reads from provider_ns table but verifies counts from reverse_ns
// This ensures accurate counts even when provider_ns has stale data
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

	// Get list of nameservers from provider_ns table
	query := "SELECT ns FROM provider_ns WHERE provider = ?"
	iter := db.Session.Query(query, provider).Iter()

	var nsList []string
	var ns string
	for iter.Scan(&ns) {
		nsList = append(nsList, ns)
	}

	if err := iter.Close(); err != nil {
		return response, err
	}

	// FIX: Get actual counts from reverse_ns table (accurate source)
	for _, ns := range nsList {
		actualCount := getActualNSCount(ctx, ns)
		response.NSCounts = append(response.NSCounts, models.NSCount{
			Nameserver: ns,
			Count:      actualCount,
		})
	}

	// Sort by count (descending)
	sort.Slice(response.NSCounts, func(i, j int) bool {
		return response.NSCounts[i].Count > response.NSCounts[j].Count
	})

	response.TotalNS = int64(len(response.NSCounts))
	response.TotalDomains = 0
	for _, ns := range response.NSCounts {
		response.TotalDomains += ns.Count
	}

	// Cache the results
	if data, marshalErr := json.Marshal(response); marshalErr == nil {
		cache.Client.Set(ctx, cacheKey, string(data), providerNSCacheTTL)
	}

	return response, nil
}

// getActualNSCount gets the real domain count from reverse_ns table
func getActualNSCount(ctx context.Context, ns string) int64 {
	var count int64
	// Try legacy table first
	query := "SELECT COUNT(*) FROM reverse_ns WHERE ns = ?"
	err := db.Session.Query(query, ns).WithContext(ctx).Scan(&count)
	if err == nil && count > 0 {
		return count
	}
	
	// If legacy has no data, try sharded table
	var shardedCount int64
	for bucket := 0; bucket < 100; bucket++ {
		var bucketCount int64
		query := "SELECT COUNT(*) FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
		err := db.Session.Query(query, ns, bucket).WithContext(ctx).Scan(&bucketCount)
		if err == nil {
			shardedCount += bucketCount
		}
	}
	
	if shardedCount > 0 {
		return shardedCount
	}
	
	// Return legacy count even if 0 (fallback)
	return count
}
