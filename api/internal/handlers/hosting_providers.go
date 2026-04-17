package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
	"github.com/soufianerochdi/revns-api/internal/models"
)

var providerGroup singleflight.Group

const (
	providerCacheTTL     = 30 * time.Minute // Increased for millions of records
	defaultProviderLimit = 100
	maxProviderLimit     = 500
)

// GetTopHostingProviders handles GET /api/v1/hosting-providers/top
func GetTopHostingProviders(c *gin.Context) {
	start := time.Now()
	limit := 10 // Top 10 by default

	// Add timeout to prevent hanging requests
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	result, err, shared := providerGroup.Do("top:10", func() (interface{}, error) {
		return fetchHostingProvidersFromTable(ctx, limit)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	providers := result.([]models.HostingProvider)

	c.JSON(http.StatusOK, models.HostingProvidersResponse{
		Providers:      providers,
		ResponseTimeMS: time.Since(start).Milliseconds(),
		Cached:         shared,
	})
}

// GetAllHostingProviders handles GET /api/v1/hosting-providers
func GetAllHostingProviders(c *gin.Context) {
	start := time.Now()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultProviderLimit)))
	if limit < 1 || limit > maxProviderLimit {
		limit = defaultProviderLimit
	}

	// Add timeout to prevent hanging requests
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Use a consistent cache key for all providers (no limit)
	result, err, shared := providerGroup.Do("all:providers", func() (interface{}, error) {
		return fetchHostingProvidersPaged(ctx, 0)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	providersPage := result.(models.HostingProvidersResponse)
	allProviders := providersPage.Providers
	total := len(allProviders)

	// Apply pagination
	offset := (page - 1) * limit
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	var providers []models.HostingProvider
	if offset < len(allProviders) {
		providers = allProviders[offset:end]
	}

	c.JSON(http.StatusOK, models.HostingProvidersResponse{
		Providers:      providers,
		Total:          total,
		Page:           page,
		Limit:          limit,
		ResponseTimeMS: time.Since(start).Milliseconds(),
		Cached:         shared,
	})
}

func fetchHostingProvidersPaged(ctx context.Context, limit int) (models.HostingProvidersResponse, error) {
	response := models.HostingProvidersResponse{}
	// Pass 0 to get all providers (no limit)
	providers, err := fetchHostingProvidersFromTable(ctx, 0)
	if err != nil {
		return response, err
	}
	response.Providers = providers
	response.Total = len(providers)
	response.Limit = limit
	response.Page = 1
	return response, nil
}

// fetchHostingProvidersFromTable reads from the pre-aggregated provider_stats table
// This is O(1) vs the previous O(n) full table scan approach
func fetchHostingProvidersFromTable(ctx context.Context, limit int) ([]models.HostingProvider, error) {
	cacheKey := "hosting-providers:all"
	if limit > 0 {
		cacheKey = "hosting-providers:top10"
	}

	// Try Redis cache first
	cachedData, err := cache.Client.Get(ctx, cacheKey).Result()
	if err == nil {
		var providers []models.HostingProvider
		if err := json.Unmarshal([]byte(cachedData), &providers); err == nil {
			if limit > 0 && len(providers) > limit {
				return providers[:limit], nil
			}
			return providers, nil
		}
	}

	// Query from provider_stats table - pre-aggregated during ingestion
	query := "SELECT provider, domain_count FROM provider_stats"
	iter := db.Session.Query(query).Iter()

	providers := make([]models.HostingProvider, 0)
	var provider string
	var domainCount int64

	for iter.Scan(&provider, &domainCount) {
		providers = append(providers, models.HostingProvider{
			ProviderName: provider,
			DomainCount:  domainCount,
		})
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	// Sort by domain count (descending) using efficient sort algorithm
	// Replaced O(n²) bubble sort with O(n log n) sort
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].DomainCount > providers[j].DomainCount
	})

	// Cache the results
	if data, err := json.Marshal(providers); err == nil {
		cache.Client.Set(ctx, cacheKey, string(data), providerCacheTTL)
	}

	if limit > 0 && len(providers) > limit {
		return providers[:limit], nil
	}

	return providers, nil
}

// extractProvider extracts the provider name from a nameserver
func extractProvider(ns string) string {
	ns = strings.ToLower(ns)

	// Common provider mappings
	providers := map[string]string{
		"cloudflare":      "Cloudflare",
		"awsdns":          "AWS",
		"aws":             "AWS",
		"amazon":          "AWS",
		"hostgator":       "HostGator",
		"godaddy":         "GoDaddy",
		"bluehost":        "Bluehost",
		"digitalocean":    "DigitalOcean",
		"linode":          "Linode",
		"vultr":           "Vultr",
		"google":          "Google",
		"googleapis":      "Google",
		"namecheap":       "Namecheap",
		"dreamhost":       "DreamHost",
		"inmotionhosting": "InMotion Hosting",
		"a2hosting":       "A2 Hosting",
		"siteground":      "SiteGround",
		"hostinger":       "Hostinger",
		"ipage":           "iPage",
		"media":           "Media Temple",
		"rackspace":       "Rackspace",
		"ovh":             "OVH",
		"hetzner":         "Hetzner",
		"cloudways":       "Cloudways",
		"kinsta":          "Kinsta",
		"wpengine":        "WP Engine",
		"pantheon":        "Pantheon",
		"fly":             "Fly.io",
		"vercel":          "Vercel",
		"netlify":         "Netlify",
		"heroku":          "Heroku",
		"fastly":          "Fastly",
		"akamai":          "Akamai",
		"edgecast":        "Edgecast",
	}

	// Check for each provider keyword
	for keyword, provider := range providers {
		if strings.Contains(ns, keyword) {
			return provider
		}
	}

	// Try to extract TLD or second-level domain
	parts := strings.Split(ns, ".")
	if len(parts) >= 2 {
		for i := len(parts) - 2; i >= 0; i-- {
			label := strings.TrimSpace(parts[i])
			if label == "" {
				continue
			}
			if isIgnoredProviderLabel(label) {
				continue
			}
			return strings.ToUpper(label[:1]) + label[1:]
		}
	}

	return "Unknown"
}

func isIgnoredProviderLabel(label string) bool {
	ignored := map[string]bool{
		"com":    true,
		"net":    true,
		"org":    true,
		"co":     true,
		"biz":    true,
		"info":   true,
		"io":     true,
		"app":    true,
		"site":   true,
		"online": true,
		"cloud":  true,
		"dev":    true,
		"uk":     true,
		"de":     true,
		"fr":     true,
		"it":     true,
		"es":     true,
		"nl":     true,
		"pl":     true,
		"ru":     true,
		"eu":     true,
	}

	if ignored[label] {
		return true
	}

	if strings.HasPrefix(label, "ns") || strings.HasPrefix(label, "dns") || strings.HasPrefix(label, "pdns") {
		return true
	}

	if len(label) <= 2 {
		return true
	}

	return false
}
