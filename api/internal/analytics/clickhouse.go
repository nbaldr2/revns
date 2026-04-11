package analytics

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

var conn driver.Conn

// InitializeClickHouse establishes connection to ClickHouse analytics database
func InitializeClickHouse(ctx context.Context, addr string) error {
	var err error
	conn, err = clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "analytics",
			Username: "default",
			Password: "",
		},
		DialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
		Debug: false,
		Debugf: func(format string, v ...interface{}) {
			log.Printf(format, v)
		},
		Settings: clickhouse.Settings{
			"max_execution_time":           60,
			"async_insert":                 "1",
			"wait_for_async_insert":        "0",
			"async_insert_busy_timeout_ms": "1000",
		},
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		DialTimeout:          time.Second * 30,
		MaxOpenConns:         10,
		MaxIdleConns:         5,
		ConnMaxLifetime:      time.Hour,
		BlockBufferSize:      10,
		MaxCompressionBuffer: 10240,
	})

	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %v", err)
	}

	if err := conn.Ping(ctx); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			log.Printf("ClickHouse exception: [%d] %s", exception.Code, exception.Message)
		}
		return fmt.Errorf("failed to ping ClickHouse: %v", err)
	}

	log.Printf("Connected to ClickHouse at %s", addr)
	return nil
}

// Close terminates the ClickHouse connection
func Close() error {
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// Ping checks if ClickHouse connection is alive
func Ping(ctx context.Context) error {
	if conn == nil {
		return fmt.Errorf("clickhouse connection is nil")
	}
	return conn.Ping(ctx)
}

// IsConnected returns whether ClickHouse is connected
func IsConnected() bool {
	return conn != nil
}

// GetConnection returns the current ClickHouse connection
func GetConnection() driver.Conn {
	return conn
}

// InsertDomainAnalytics inserts domain analytics data into ClickHouse
func InsertDomainAnalytics(ctx context.Context, domain string, nameservers []string, level string, provider string) error {
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO domain_analytics (domain, nameservers, level, provider, timestamp)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %v", err)
	}

	nsList := strings.Join(nameservers, ",")

	err = batch.Append(
		domain,
		nsList,
		level,
		provider,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to append to batch: %v", err)
	}

	return batch.Send()
}

// InsertNSStats inserts nameserver statistics
func InsertNSStats(ctx context.Context, ns string, domainCount int64, provider string) error {
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO ns_stats (ns, domain_count, provider, date)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %v", err)
	}

	err = batch.Append(
		ns,
		domainCount,
		provider,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to append to batch: %v", err)
	}

	return batch.Send()
}

// InsertProviderStats inserts provider statistics
func InsertProviderStats(ctx context.Context, provider string, domainCount int64, nsCount int64) error {
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO provider_analytics (provider, domain_count, ns_count, date)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %v", err)
	}

	err = batch.Append(
		provider,
		domainCount,
		nsCount,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to append to batch: %v", err)
	}

	return batch.Send()
}

// GetDomainAnalytics queries domain analytics with time range
func GetDomainAnalytics(ctx context.Context, startDate, endDate time.Time, limit int) ([]DomainAnalyticsData, error) {
	rows, err := conn.Query(ctx, `
		SELECT 
			domain, 
			nameservers, 
			level, 
			provider, 
			timestamp
		FROM domain_analytics
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, startDate, endDate, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query domain analytics: %v", err)
	}
	defer rows.Close()

	var results []DomainAnalyticsData
	for rows.Next() {
		var data DomainAnalyticsData
		if err := rows.Scan(&data.Domain, &data.Nameservers, &data.Level, &data.Provider, &data.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}

	return results, nil
}

// GetNSAnalytics queries nameserver analytics with time range
func GetNSAnalytics(ctx context.Context, startDate, endDate time.Time) ([]NSAnalyticsData, error) {
	rows, err := conn.Query(ctx, `
		SELECT 
			ns,
			domain_count,
			provider,
			date
		FROM ns_stats
		WHERE date >= ? AND date <= ?
		ORDER BY domain_count DESC
	`, startDate, endDate)

	if err != nil {
		return nil, fmt.Errorf("failed to query ns analytics: %v", err)
	}
	defer rows.Close()

	var results []NSAnalyticsData
	for rows.Next() {
		var data NSAnalyticsData
		if err := rows.Scan(&data.NS, &data.DomainCount, &data.Provider, &data.Date); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}

	return results, nil
}

// GetProviderAnalytics queries provider analytics
func GetProviderAnalytics(ctx context.Context, startDate, endDate time.Time, limit int) ([]ProviderAnalyticsData, error) {
	rows, err := conn.Query(ctx, `
		SELECT 
			provider,
			domain_count,
			ns_count,
			date
		FROM provider_analytics
		WHERE date >= ? AND date <= ?
		ORDER BY domain_count DESC
		LIMIT ?
	`, startDate, endDate, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query provider analytics: %v", err)
	}
	defer rows.Close()

	var results []ProviderAnalyticsData
	for rows.Next() {
		var data ProviderAnalyticsData
		if err := rows.Scan(&data.Provider, &data.DomainCount, &data.NSCount, &data.Date); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}

	return results, nil
}

// GetDailyDomainGrowth retrieves daily domain growth statistics
func GetDailyDomainGrowth(ctx context.Context, days int) ([]DailyStats, error) {
	rows, err := conn.Query(ctx, `
		SELECT 
			toDate(timestamp) as date,
			count() as total_domains,
			count(DISTINCT provider) as total_providers
		FROM domain_analytics
		WHERE timestamp >= now() - INTERVAL ? DAY
		GROUP BY date
		ORDER BY date DESC
	`, days)

	if err != nil {
		return nil, fmt.Errorf("failed to query daily growth: %v", err)
	}
	defer rows.Close()

	var results []DailyStats
	for rows.Next() {
		var data DailyStats
		if err := rows.Scan(&data.Date, &data.TotalDomains, &data.TotalProviders); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}

	return results, nil
}

// GetTopNameservers retrieves top nameservers by domain count
func GetTopNameservers(ctx context.Context, limit int) ([]TopNSStats, error) {
	rows, err := conn.Query(ctx, `
		SELECT 
			ns,
			sum(domain_count) as total_domains,
			argMax(provider, date) as current_provider
		FROM ns_stats
		WHERE date >= now() - INTERVAL 7 DAY
		GROUP BY ns
		ORDER BY total_domains DESC
		LIMIT ?
	`, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query top nameservers: %v", err)
	}
	defer rows.Close()

	var results []TopNSStats
	for rows.Next() {
		var data TopNSStats
		if err := rows.Scan(&data.NS, &data.TotalDomains, &data.Provider); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}

	return results, nil
}

// Data structures for analytics responses
type DomainAnalyticsData struct {
	Domain      string    `json:"domain"`
	Nameservers string    `json:"nameservers"`
	Level       string    `json:"level"`
	Provider    string    `json:"provider"`
	Timestamp   time.Time `json:"timestamp"`
}

type NSAnalyticsData struct {
	NS          string    `json:"ns"`
	DomainCount int64     `json:"domain_count"`
	Provider    string    `json:"provider"`
	Date        time.Time `json:"date"`
}

type ProviderAnalyticsData struct {
	Provider    string    `json:"provider"`
	DomainCount int64     `json:"domain_count"`
	NSCount     int64     `json:"ns_count"`
	Date        time.Time `json:"date"`
}

type DailyStats struct {
	Date           time.Time `json:"date"`
	TotalDomains   int64     `json:"total_domains"`
	TotalProviders int64     `json:"total_providers"`
}

type TopNSStats struct {
	NS           string `json:"ns"`
	TotalDomains int64  `json:"total_domains"`
	Provider     string `json:"provider"`
}
