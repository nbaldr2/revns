package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soufianerochdi/revns-api/internal/db"
)

type GlobalStatsResponse struct {
	Providers       int64 `json:"providers"`
	Nameservers     int64 `json:"nameservers"`
	TotalDomains    int64 `json:"total_domains"`
	ResponseTimeMS  int64 `json:"response_time_ms"`
}

// GetGlobalStats handles GET /api/v1/stats
func GetGlobalStats(c *gin.Context) {
	start := time.Now()

	if db.Session == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database session is not initialized"})
		return
	}

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

	c.JSON(http.StatusOK, GlobalStatsResponse{
		Providers:      providersCount,
		Nameservers:    nsCount,
		TotalDomains:   totalDomains,
		ResponseTimeMS: time.Since(start).Milliseconds(),
	})
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