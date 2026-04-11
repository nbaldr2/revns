package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soufianerochdi/revns-api/internal/analytics"
	"github.com/soufianerochdi/revns-api/internal/cache"
)

// GetAnalyticsDashboard handles GET /api/v1/analytics/dashboard
func GetAnalyticsDashboard(c *gin.Context) {
	start := time.Now()

	// Parse query parameters
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	// Check cache first
	cacheKey := fmt.Sprintf("analytics:dashboard:%d", days)
	cachedData, err := cache.Client.Get(c.Request.Context(), cacheKey).Result()
	if err == nil {
		c.Data(http.StatusOK, "application/json", []byte(cachedData))
		return
	}

	// Fetch analytics data
	var dashboard DashboardAnalytics

	// Get daily growth statistics
	dailyStats, err := analytics.GetDailyDomainGrowth(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch daily stats: " + err.Error()})
		return
	}
	dashboard.DailyStats = dailyStats

	// Get top nameservers
	topNameservers, err := analytics.GetTopNameservers(c.Request.Context(), 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch top nameservers: " + err.Error()})
		return
	}
	dashboard.TopNameservers = topNameservers

	// Calculate summary statistics
	if len(dailyStats) > 0 {
		firstDay := dailyStats[0]
		lastDay := dailyStats[len(dailyStats)-1]
		dashboard.Summary = SummaryStats{
			TotalDomains:   lastDay.TotalDomains,
			TotalProviders: lastDay.TotalProviders,
			GrowthRate:     calculateGrowthRate(firstDay.TotalDomains, lastDay.TotalDomains),
			DataPoints:     int64(len(dailyStats)),
		}
	}

	dashboard.ResponseTimeMS = time.Since(start).Milliseconds()

	// Cache the response
	responseData, _ := json.Marshal(dashboard)
	cache.Client.Set(c.Request.Context(), cacheKey, string(responseData), 5*time.Minute)

	c.JSON(http.StatusOK, dashboard)
}

// GetDomainAnalytics handles GET /api/v1/analytics/domains
func GetDomainAnalytics(c *gin.Context) {
	start := time.Now()

	// Parse query parameters
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit < 1 || limit > 1000 {
		limit = 100
	}

	startDate := time.Now().AddDate(0, 0, -days)
	endDate := time.Now()

	// Fetch domain analytics
	domains, err := analytics.GetDomainAnalytics(c.Request.Context(), startDate, endDate, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domain analytics: " + err.Error()})
		return
	}

	// Split nameservers back to array
	var response []DomainAnalyticsResponse
	for _, domain := range domains {
		response = append(response, DomainAnalyticsResponse{
			Domain:      domain.Domain,
			Nameservers: strings.Split(domain.Nameservers, ","),
			Level:       domain.Level,
			Provider:    domain.Provider,
			Timestamp:   domain.Timestamp,
		})
	}

	c.JSON(http.StatusOK, DomainAnalyticsListResponse{
		Domains:        response,
		Total:          int64(len(response)),
		Days:           int64(days),
		ResponseTimeMS: time.Since(start).Milliseconds(),
	})
}

// GetNSAnalytics handles GET /api/v1/analytics/nameservers
func GetNSAnalytics(c *gin.Context) {
	start := time.Now()

	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	startDate := time.Now().AddDate(0, 0, -days)
	endDate := time.Now()

	// Fetch nameserver analytics
	nsStats, err := analytics.GetNSAnalytics(c.Request.Context(), startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nameserver analytics: " + err.Error()})
		return
	}

	// Aggregate by nameserver
	nsMap := make(map[string]*NSAggregatedStats)
	for _, stat := range nsStats {
		if existing, ok := nsMap[stat.NS]; ok {
			existing.TotalDomains += stat.DomainCount
			existing.DataPoints++
		} else {
			nsMap[stat.NS] = &NSAggregatedStats{
				NS:           stat.NS,
				Provider:     stat.Provider,
				TotalDomains: stat.DomainCount,
				DataPoints:   1,
				FirstSeen:    stat.Date,
				LastSeen:     stat.Date,
			}
		}
	}

	// Convert map to slice
	var response []NSAggregatedStats
	for _, stats := range nsMap {
		response = append(response, *stats)
	}

	// Sort by total domains
	sort.Slice(response, func(i, j int) bool {
		return response[i].TotalDomains > response[j].TotalDomains
	})

	c.JSON(http.StatusOK, NSAnalyticsListResponse{
		Nameservers:    response,
		Total:          int64(len(response)),
		Days:           int64(days),
		ResponseTimeMS: time.Since(start).Milliseconds(),
	})
}

// GetProviderAnalytics handles GET /api/v1/analytics/providers
func GetProviderAnalytics(c *gin.Context) {
	start := time.Now()

	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}

	startDate := time.Now().AddDate(0, 0, -days)
	endDate := time.Now()

	// Fetch provider analytics
	providerStats, err := analytics.GetProviderAnalytics(c.Request.Context(), startDate, endDate, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch provider analytics: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, ProviderAnalyticsListResponse{
		Providers:      providerStats,
		Total:          int64(len(providerStats)),
		Days:           int64(days),
		ResponseTimeMS: time.Since(start).Milliseconds(),
	})
}

// GetAnalyticsSummary handles GET /api/v1/analytics/summary
func GetAnalyticsSummary(c *gin.Context) {
	start := time.Now()

	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 || days > 90 {
		days = 7
	}

	startDate := time.Now().AddDate(0, 0, -days)
	endDate := time.Now()

	// Get daily stats for summary
	dailyStats, err := analytics.GetDailyDomainGrowth(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch summary: " + err.Error()})
		return
	}

	var summary AnalyticsSummary
	if len(dailyStats) > 0 {
		current := dailyStats[0]
		oldest := dailyStats[len(dailyStats)-1]

		summary = AnalyticsSummary{
			CurrentStats: SummaryStats{
				TotalDomains:   current.TotalDomains,
				TotalProviders: current.TotalProviders,
				DataPoints:     int64(len(dailyStats)),
			},
			Growth: GrowthMetrics{
				DomainGrowth:    current.TotalDomains - oldest.TotalDomains,
				ProviderGrowth:  current.TotalProviders - oldest.TotalProviders,
				DomainGrowthPct: calculateGrowthRate(oldest.TotalDomains, current.TotalDomains),
			},
			TimeRange: TimeRange{
				Days:      int64(days),
				StartDate: startDate,
				EndDate:   endDate,
			},
		}
	}

	// Get top providers
	topProviders, err := analytics.GetProviderAnalytics(c.Request.Context(), startDate, endDate, 5)
	if err == nil && len(topProviders) > 0 {
		summary.TopProviders = topProviders
	}

	summary.ResponseTimeMS = time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, summary)
}

// Helper functions
func calculateGrowthRate(old, new int64) float64 {
	if old == 0 {
		return 0
	}
	return float64(new-old) / float64(old) * 100
}

// Data structures
type DashboardAnalytics struct {
	Summary        SummaryStats              `json:"summary"`
	DailyStats     []analytics.DailyStats    `json:"daily_stats"`
	TopNameservers []analytics.TopNSStats    `json:"top_nameservers"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
}

type SummaryStats struct {
	TotalDomains   int64   `json:"total_domains"`
	TotalProviders int64   `json:"total_providers"`
	GrowthRate     float64 `json:"growth_rate"`
	DataPoints     int64   `json:"data_points"`
}

type DomainAnalyticsResponse struct {
	Domain      string    `json:"domain"`
	Nameservers []string  `json:"nameservers"`
	Level       string    `json:"level"`
	Provider    string    `json:"provider"`
	Timestamp   time.Time `json:"timestamp"`
}

type DomainAnalyticsListResponse struct {
	Domains        []DomainAnalyticsResponse `json:"domains"`
	Total          int64                     `json:"total"`
	Days           int64                     `json:"days"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
}

type NSAggregatedStats struct {
	NS           string    `json:"ns"`
	Provider     string    `json:"provider"`
	TotalDomains int64     `json:"total_domains"`
	DataPoints   int64     `json:"data_points"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

type NSAnalyticsListResponse struct {
	Nameservers    []NSAggregatedStats `json:"nameservers"`
	Total          int64               `json:"total"`
	Days           int64               `json:"days"`
	ResponseTimeMS int64               `json:"response_time_ms"`
}

type ProviderAnalyticsListResponse struct {
	Providers      []analytics.ProviderAnalyticsData `json:"providers"`
	Total          int64                             `json:"total"`
	Days           int64                             `json:"days"`
	ResponseTimeMS int64                             `json:"response_time_ms"`
}

type AnalyticsSummary struct {
	CurrentStats   SummaryStats                      `json:"current_stats"`
	Growth         GrowthMetrics                     `json:"growth"`
	TimeRange      TimeRange                         `json:"time_range"`
	TopProviders   []analytics.ProviderAnalyticsData `json:"top_providers,omitempty"`
	ResponseTimeMS int64                             `json:"response_time_ms"`
}

type GrowthMetrics struct {
	DomainGrowth    int64   `json:"domain_growth"`
	ProviderGrowth  int64   `json:"provider_growth"`
	DomainGrowthPct float64 `json:"domain_growth_pct"`
}

type TimeRange struct {
	Days      int64     `json:"days"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}
