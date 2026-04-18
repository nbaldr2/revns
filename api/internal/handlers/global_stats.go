package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soufianerochdi/revns-api/internal/db"
)

type GlobalStatsResponse struct {
	Providers       int64 `json:"providers"`
	Nameservers     int64 `json:"nameservers"`
	TotalRecords    int64 `json:"total_records"`      // Total domain-provider associations (13M+)
	UniqueDomains   int64 `json:"unique_domains"`     // Actual unique domains (from metadata_v2)
	ResponseTimeMS  int64 `json:"response_time_ms"`
	DataSource      string `json:"data_source"`      // 'live' or 'cached'
}

type GlobalStatV2 struct {
	StatName          string    `json:"stat_name"`
	StatValue         int64     `json:"stat_value"`
	LastComputed      time.Time `json:"last_computed"`
	ComputationMethod string    `json:"computation_method"`
}

// GetGlobalStats handles GET /api/v1/stats
// Returns both total_records (all associations) and unique_domains (actual unique domains)
func GetGlobalStats(c *gin.Context) {
	start := time.Now()

	if db.Session == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database session is not initialized"})
		return
	}

	// Try to get accurate stats from global_stats_v2 first
	stats, err := getAccurateStats()
	if err == nil && stats.UniqueDomains > 0 {
		// Use the accurate computed stats
		c.JSON(http.StatusOK, GlobalStatsResponse{
			Providers:      stats.Providers,
			Nameservers:    stats.Nameservers,
			TotalRecords:   stats.TotalRecords,
			UniqueDomains:  stats.UniqueDomains,
			ResponseTimeMS: time.Since(start).Milliseconds(),
			DataSource:     "computed_v2",
		})
		return
	}

	// Fallback to live counting
	providersCount, err := countTable("provider_stats")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	nsCount, err := countTable("provider_ns")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get total records (sum from provider_stats)
	var totalRecords int64
	if err := db.Session.Query("SELECT sum(domain_count) FROM provider_stats").Scan(&totalRecords); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get unique domains from pre-computed global_stats_v2 (fast, no timeout)
	var uniqueDomains int64
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	// Try pre-computed stats first (fast O(1) lookup)
	if err := db.Session.Query("SELECT stat_value FROM global_stats_v2 WHERE stat_name = 'unique_domains'").WithContext(ctx).Scan(&uniqueDomains); err != nil {
		uniqueDomains = 0 // Not computed yet
	}

	c.JSON(http.StatusOK, GlobalStatsResponse{
		Providers:      providersCount,
		Nameservers:    nsCount,
		TotalRecords:   totalRecords,
		UniqueDomains:  uniqueDomains,
		ResponseTimeMS: time.Since(start).Milliseconds(),
		DataSource:     "live_fast",
	})
}

// getAccurateStats retrieves pre-computed accurate stats from global_stats_v2
func getAccurateStats() (*GlobalStatsResponse, error) {
	var response GlobalStatsResponse
	var found bool

	// Get each stat individually from global_stats_v2 table
	// The table has columns: stat_name, stat_value, last_computed, computation_method
	
	// Total records
	iter := db.Session.Query("SELECT stat_value FROM global_stats_v2 WHERE stat_name = 'total_domain_records'").Iter()
	if iter.Scan(&response.TotalRecords) {
		found = true
	}
	iter.Close()

	// Unique domains
	iter = db.Session.Query("SELECT stat_value FROM global_stats_v2 WHERE stat_name = 'unique_domains'").Iter()
	if iter.Scan(&response.UniqueDomains) {
		found = true
	}
	iter.Close()

	// Providers
	iter = db.Session.Query("SELECT stat_value FROM global_stats_v2 WHERE stat_name = 'providers'").Iter()
	if iter.Scan(&response.Providers) {
		found = true
	}
	iter.Close()

	// Nameservers
	iter = db.Session.Query("SELECT stat_value FROM global_stats_v2 WHERE stat_name = 'nameservers'").Iter()
	if iter.Scan(&response.Nameservers) {
		found = true
	}
	iter.Close()

	if !found {
		return nil, fmt.Errorf("no stats found in global_stats_v2")
	}

	return &response, nil
}

func countTable(table string) (int64, error) {
	if db.Session == nil {
		return 0, http.ErrServerClosed // or any appropriate error, handled above
	}
	var count int64
	if err := db.Session.Query("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}