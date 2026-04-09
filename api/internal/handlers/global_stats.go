package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
)

type GlobalStatsResponse struct {
	Providers       int64 `json:"providers"`
	Nameservers     int64 `json:"nameservers"`
	TotalDomains    int64 `json:"total_domains"`
	TotalRows       int64 `json:"total_rows"`     // Raw rows uploaded (includes duplicates)
	ResponseTimeMS  int64 `json:"response_time_ms"`
}

// GetGlobalStats handles GET /api/v1/stats
func GetGlobalStats(c *gin.Context) {
	start := time.Now()

	providersCount, err := countTable("provider_stats")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Use provider_ns as a proxy for total nameservers
	nsCount, err := countTable("provider_ns")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sum total domains from provider_stats
	var totalDomains int64
	if err := db.Session.Query("SELECT sum(domain_count) FROM provider_stats").Scan(&totalDomains); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get total rows from Redis cache (fast, avoids COUNT(*) timeout on 3.5M+ rows)
	totalRows, err := cache.GetTotalRows(context.Background())
	if err != nil {
		// Don't fail if Redis error, just return 0
		totalRows = 0
	}

	c.JSON(http.StatusOK, GlobalStatsResponse{
		Providers:      providersCount,
		Nameservers:    nsCount,
		TotalDomains:   totalDomains,
		TotalRows:      totalRows,
		ResponseTimeMS: time.Since(start).Milliseconds(),
	})
}

func countTable(table string) (int64, error) {
	var count int64
	if err := db.Session.Query("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}