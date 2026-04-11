package handlers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
)

var providerDomainGroup singleflight.Group

const providerDomainCacheTTL = 10 * time.Minute
const providerDomainCacheVersion = "v3"

// DomainNSMapping represents a domain to its nameserver mapping
type DomainNSMapping struct {
	Domain     string `json:"domain"`
	Nameserver string `json:"nameserver"`
}

// ProviderDomainSearchResponse represents the response for provider domain search
type ProviderDomainSearchResponse struct {
	ProviderDomain string            `json:"provider_domain"`
	Nameservers    []string          `json:"nameservers"`
	TotalDomains   int64             `json:"total"`
	Returned       int64             `json:"returned"`
	Domains        []DomainNSMapping `json:"domains"`
	ResponseTimeMS int64             `json:"response_time_ms"`
}

// GetProviderDomainSearch handles GET /api/v1/provider-search?domain=<provider_domain>
func GetProviderDomainSearch(c *gin.Context) {
	start := time.Now()
	providerDomain := strings.TrimSpace(c.Query("domain"))

	// Validate input
	if providerDomain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain parameter is required"})
		return
	}

	// Check for spaces
	if strings.Contains(providerDomain, " ") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain must not contain spaces"})
		return
	}

	// Strip any "ns1.", "ns2.", "ns3.", etc. prefixes if accidentally provided
	providerDomain = stripNSPrefix(providerDomain)

	// Normalize: remove www. prefix if present
	providerDomain = strings.TrimPrefix(providerDomain, "www.")

	// Validate domain format (basic check)
	if !isValidDomainFormat(providerDomain) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain format"})
		return
	}

	limit := 10000 // Default limit for provider domain search
	if l := c.Query("limit"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &limit); err == nil && parsed == 1 {
			if limit < 1 {
				limit = 10000
			}
		}
	}

	cacheKey := strings.ToLower(providerDomain)
	result, err, shared := providerDomainGroup.Do("provider-domain:"+providerDomainCacheVersion+":"+cacheKey, func() (interface{}, error) {
		return fetchProviderDomains(c.Request.Context(), providerDomain, limit)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := result.(ProviderDomainSearchResponse)
	response.ResponseTimeMS = time.Since(start).Milliseconds()

	// Add cache header if result was shared/coalesced
	if shared {
		c.Header("X-Cache", "HIT")
	}

	c.JSON(http.StatusOK, response)
}

// GetProviderDomainSearchCSV handles GET /api/v1/provider-search.csv?domain=<provider_domain>
// It streams all results as CSV to avoid UI limits.
func GetProviderDomainSearchCSV(c *gin.Context) {
	providerDomain := strings.TrimSpace(c.Query("domain"))
	if providerDomain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain parameter is required"})
		return
	}

	if strings.Contains(providerDomain, " ") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain must not contain spaces"})
		return
	}

	providerDomain = stripNSPrefix(providerDomain)
	providerDomain = strings.TrimPrefix(providerDomain, "www.")
	if !isValidDomainFormat(providerDomain) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain format"})
		return
	}

	filename := fmt.Sprintf("provider-search-%s.csv", providerDomain)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	writer := c.Writer
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("domain\n"))

	flusher, ok := writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	streamProviderDomainCSV(c.Request.Context(), writer, flusher, providerDomain)
}

func streamProviderDomainCSV(ctx context.Context, writer io.Writer, flusher http.Flusher, providerDomain string) {
	searchTerm := strings.ToLower(providerDomain)

	nsQuery := "SELECT ns FROM provider_ns"
	nsIter := db.Session.Query(nsQuery).Iter()

	var matchingNS []string
	var ns string
	for nsIter.Scan(&ns) {
		nsLower := strings.ToLower(ns)
		if strings.HasSuffix(nsLower, searchTerm) {
			if len(nsLower) > len(searchTerm) {
				prefixLen := len(nsLower) - len(searchTerm)
				if prefixLen > 0 && nsLower[prefixLen-1] == '.' {
					matchingNS = append(matchingNS, ns)
				}
			}
		}
	}
	_ = nsIter.Close()

	seenNS := make(map[string]bool)
	csvWriter := csv.NewWriter(writer)
	rowCount := 0

	for _, ns := range matchingNS {
		if ctx.Err() != nil {
			return
		}
		nsLower := strings.ToLower(ns)
		if seenNS[nsLower] {
			continue
		}
		seenNS[nsLower] = true

		rowCount += streamDomainsForNS(ctx, csvWriter, ns)
		csvWriter.Flush()
		flusher.Flush()
	}

	_ = rowCount
}

func streamDomainsForNS(ctx context.Context, csvWriter *csv.Writer, ns string) int {
	rowCount := 0
	query := "SELECT domain FROM reverse_ns WHERE ns = ?"
	iter := db.Session.Query(query, ns).WithContext(ctx).Iter()

	var domain string
	for iter.Scan(&domain) {
		if ctx.Err() != nil {
			_ = iter.Close()
			return rowCount
		}
		_ = csvWriter.Write([]string{domain})
		rowCount++
	}
	_ = iter.Close()

	for bucket := 0; bucket < 100; bucket++ {
		bucketQuery := "SELECT domain FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
		bucketIter := db.Session.Query(bucketQuery, ns, bucket).WithContext(ctx).Iter()
		for bucketIter.Scan(&domain) {
			if ctx.Err() != nil {
				_ = bucketIter.Close()
				return rowCount
			}
			_ = csvWriter.Write([]string{domain})
			rowCount++
		}
		_ = bucketIter.Close()
	}

	return rowCount
}

// stripNSPrefix removes common nameserver prefixes like ns1., ns2., etc.
func stripNSPrefix(domain string) string {
	// Pattern to match common NS prefixes followed by a domain
	nsPrefixPattern := regexp.MustCompile(`^ns\d+\.`)
	return nsPrefixPattern.ReplaceAllString(domain, "")
}

// isValidDomainFormat performs basic domain format validation
func isValidDomainFormat(domain string) bool {
	// Must have at least one dot
	if !strings.Contains(domain, ".") {
		return false
	}

	// Must not start or end with dot
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	// Basic length check
	if len(domain) < 3 || len(domain) > 253 {
		return false
	}

	// Check for valid characters (alphanumeric, dots, hyphens)
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`)
	return validPattern.MatchString(domain)
}

// fetchProviderDomains finds all domains using nameservers that end with the provider domain (wildcard/subdomain search)
func fetchProviderDomains(ctx context.Context, providerDomain string, limit int) (ProviderDomainSearchResponse, error) {
	response := ProviderDomainSearchResponse{
		ProviderDomain: providerDomain,
		Nameservers:    make([]string, 0),
		Domains:        make([]DomainNSMapping, 0),
	}

	// Normalize the search term
	searchTerm := strings.ToLower(providerDomain)

	// Try cache first
	cacheKey := fmt.Sprintf("provider-domain:%s:%s", providerDomainCacheVersion, searchTerm)
	cachedData, err := cache.Client.Get(ctx, cacheKey).Result()
	if err == nil {
		var cachedResponse ProviderDomainSearchResponse
		if unmarshalErr := json.Unmarshal([]byte(cachedData), &cachedResponse); unmarshalErr == nil {
			valid := cachedResponse.Domains != nil && cachedResponse.Returned == int64(len(cachedResponse.Domains))
			if valid {
				for _, mapping := range cachedResponse.Domains {
					if mapping.Domain == "" || mapping.Nameserver == "" {
						valid = false
						break
					}
				}
			}
			if valid {
				return cachedResponse, nil
			}
		}
	} else if err != redis.Nil {
		return response, err
	}

	// Search for nameservers that END with the provider domain (wildcard/subdomain search)
	// This matches ns1.cloudflare.com, ns2.cloudflare.com, test.cloudflare.com, etc.
	nsQuery := "SELECT ns FROM provider_ns"
	nsIter := db.Session.Query(nsQuery).Iter()

	var matchingNS []string
	var ns string
	for nsIter.Scan(&ns) {
		nsLower := strings.ToLower(ns)
		// Check if the nameserver ENDS with the search term (suffix match)
		// This ensures we match all subdomains of the provider domain
		if strings.HasSuffix(nsLower, searchTerm) {
			// Additional check to ensure it's a proper subdomain match
			// e.g., "cloudflare.com" should match "ns1.cloudflare.com" but not "notcloudflare.com"
			if len(nsLower) > len(searchTerm) {
				// Ensure there's a dot before the provider domain
				prefixLen := len(nsLower) - len(searchTerm)
				if prefixLen > 0 && nsLower[prefixLen-1] == '.' {
					matchingNS = append(matchingNS, ns)
				}
			}
		}
	}
	if err := nsIter.Close(); err != nil {
		return response, fmt.Errorf("failed to query nameservers: %w", err)
	}

	// Deduplicate nameservers while preserving order
	seenNS := make(map[string]bool)
	uniqueNS := make([]string, 0, len(matchingNS))
	for _, ns := range matchingNS {
		nsLower := strings.ToLower(ns)
		if !seenNS[nsLower] {
			seenNS[nsLower] = true
			uniqueNS = append(uniqueNS, ns)
		}
	}
	matchingNS = uniqueNS
	response.Nameservers = matchingNS

	var totalMatches int64
	for _, ns := range matchingNS {
		totalMatches += countDomainsForNS(ctx, ns)
	}

	// If no matching nameservers found, return empty result
	if len(matchingNS) == 0 {
		return response, nil
	}

	// Fetch domains for each matching nameserver, tracking which NS serves each domain
	domainMappings := make([]DomainNSMapping, 0, limit)
	seenDomains := make(map[string]bool)

	for _, ns := range matchingNS {
		domains, err := fetchDomainsForNS(ctx, ns, limit)
		if err != nil {
			// Log error but continue with other nameservers
			continue
		}
		for _, domain := range domains {
			domainLower := strings.ToLower(domain)
			// Only add if we haven't seen this domain yet
			if !seenDomains[domainLower] {
				seenDomains[domainLower] = true
				domainMappings = append(domainMappings, DomainNSMapping{
					Domain:     domain,
					Nameserver: ns,
				})
			}
			// Check if we've reached the limit
			if len(domainMappings) >= limit {
				break
			}
		}
		if len(domainMappings) >= limit {
			break
		}
	}

	// Sort alphabetically by domain name
	sort.Slice(domainMappings, func(i, j int) bool {
		return strings.ToLower(domainMappings[i].Domain) < strings.ToLower(domainMappings[j].Domain)
	})

	// Apply limit
	if len(domainMappings) > limit {
		domainMappings = domainMappings[:limit]
	}

	response.Domains = domainMappings
	response.TotalDomains = totalMatches
	response.Returned = int64(len(domainMappings))

	// Cache the results
	if data, marshalErr := json.Marshal(response); marshalErr == nil {
		cache.Client.Set(ctx, cacheKey, string(data), providerDomainCacheTTL)
	}

	return response, nil
}

// fetchDomainsForNS fetches all domains for a given nameserver
func fetchDomainsForNS(ctx context.Context, ns string, limit int) ([]string, error) {
	// Try legacy table first
	query := "SELECT domain FROM reverse_ns WHERE ns = ? LIMIT ?"
	iter := db.Session.Query(query, ns, limit).WithContext(ctx).Iter()

	domains := make([]string, 0)
	var domain string
	for iter.Scan(&domain) {
		domains = append(domains, domain)
	}
	if err := iter.Close(); err == nil && len(domains) > 0 {
		return domains, nil
	}

	// Try sharded table if legacy table has no data
	for bucket := 0; bucket < 100; bucket++ {
		bucketQuery := "SELECT domain FROM reverse_ns_sharded WHERE ns = ? AND bucket = ? LIMIT ?"
		bucketIter := db.Session.Query(bucketQuery, ns, bucket, limit).WithContext(ctx).Iter()

		for bucketIter.Scan(&domain) {
			domains = append(domains, domain)
		}
		if err := bucketIter.Close(); err != nil {
			continue
		}

		if len(domains) >= limit {
			break
		}
	}

	return domains, nil
}

// countDomainsForNS returns the total domain count for a nameserver across legacy and sharded tables.
func countDomainsForNS(ctx context.Context, ns string) int64 {
	var count int64
	query := "SELECT COUNT(*) FROM reverse_ns WHERE ns = ?"
	err := db.Session.Query(query, ns).WithContext(ctx).Scan(&count)
	if err == nil && count > 0 {
		return count
	}

	var shardedCount int64
	for bucket := 0; bucket < 100; bucket++ {
		var bucketCount int64
		query := "SELECT COUNT(*) FROM reverse_ns_sharded WHERE ns = ? AND bucket = ?"
		if err := db.Session.Query(query, ns, bucket).WithContext(ctx).Scan(&bucketCount); err == nil {
			shardedCount += bucketCount
		}
	}

	if shardedCount > 0 {
		return shardedCount
	}

	return count
}
