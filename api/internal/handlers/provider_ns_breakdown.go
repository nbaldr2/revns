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

// fetchProviderNSBreakdownFromTable reads from the pre-aggregated provider_ns table
// This is O(1) for a single provider vs the previous O(n) full table scan
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

	// Query from provider_ns table - pre-aggregated during ingestion
	// This table has provider as partition key and ns as clustering key
	query := "SELECT ns, domain_count FROM provider_ns WHERE provider = ?"
	iter := db.Session.Query(query, provider).Iter()

	var ns string
	var domainCount int64

	for iter.Scan(&ns, &domainCount) {
		response.NSCounts = append(response.NSCounts, models.NSCount{
			Nameserver: ns,
			Count:      domainCount,
		})
	}

	if err := iter.Close(); err != nil {
		return response, err
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
