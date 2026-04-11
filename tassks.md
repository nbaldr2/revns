a hybrid ScyllaDB + ClickHouse architecture is the optimal path to fix the performance bottlenecks identified in your code review. This isn't just about speed—it's about solving the fundamental architectural mismatches causing your critical issues.
Why Hybrid Fixes Your Critical Problems
The original review identified 7 critical issues—a hybrid approach directly solves 5 of them:
Table
Critical Issue	Root Cause (Current)	Hybrid Solution
O(n²) Bubble Sort	Sorting providers in application code	ClickHouse ORDER BY with merge sort
Memory Leak in Upload	Unbounded uploadStatuses map buffering	ClickHouse async inserts (no app-side buffering)
Cache Stampede	Singleflight + Redis complexity	ClickHouse fast enough to skip cache for stats
SQL Injection	String concatenation in CQL	ClickHouse query parameters + schema validation
Unbounded Memory Growth	Loading all domains into slice	ClickHouse streaming aggregation
The Workload Separation Strategy
plain
Copy
┌─────────────────────────────────────────────────────────────┐
│                     REVNS Hybrid Architecture               │
├─────────────────────────────────────────────────────────────┤
│  OPERATIONAL LAYER (ScyllaDB)      │  ANALYTICAL LAYER      │
│  ├─ Real-time domain lookups       │  (ClickHouse)          │
│  ├─ Nameserver existence checks    │  ├─ Provider rankings  │
│  ├─ Individual record CRUD         │  ├─ Global statistics  │
│  ├─ Primary data store (source)    │  ├─ Trend analysis     │
│  └─ Low latency point queries      │  └─ Bulk aggregations  │
│                                    │                        │
│  Write Path: CDC Replication ─────▶│  Read Path: SQL        │
└────────────────────────────────────┴────────────────────────┘
Rule of thumb: If the query touches >10,000 rows, use ClickHouse. If it touches <100 rows, use ScyllaDB.


 1: Deploy ClickHouse Instance
bash
Copy
# Docker (dev) or production Helm chart
docker run -d --name clickhouse-revns \
  --ulimit nofile=262144:262144 \
  -p 8123:8123 -p 9000:9000 \
  -v clickhouse_data:/var/lib/clickhouse \
  clickhouse/clickhouse-server:latest
Validation: curl http://localhost:8123/ping returns Ok.
Task 2: Create Analytics Schema
Connect to ClickHouse (port 8123 or 9000) and execute:
sql
Copy
-- Main table for domain mappings
CREATE TABLE reverse_ns_analytics (
    domain String,
    nameserver LowCardinality(String),
    provider LowCardinality(String),
    bucket UInt8,
    ingestion_time DateTime DEFAULT now(),
    date Date DEFAULT today()
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(date)
ORDER BY (provider, nameserver, domain)
TTL date + INTERVAL 90 DAY;  -- Auto-cleanup old data

-- Materialized view: Instant provider stats (replaces bubble sort)
CREATE MATERIALIZED VIEW provider_stats_mv
ENGINE = SummingMergeTree()
ORDER BY provider
POPULATE AS SELECT 
    provider,
    count() as domain_count,
    uniqExact(nameserver) as unique_nameservers,
    max(ingestion_time) as last_updated
FROM reverse_ns_analytics
GROUP BY provider;

-- Nameserver index for fast lookups
CREATE TABLE nameserver_domains (
    nameserver LowCardinality(String),
    domain String,
    date Date
) ENGINE = MergeTree()
ORDER BY (nameserver, domain)
TTL date + INTERVAL 30 DAY;
Validation: SHOW TABLES returns all three tables.
Task 3: Install Go ClickHouse Driver
bash
Copy
go get github.com/ClickHouse/clickhouse-go/v2
Add to api/internal/db/clickhouse.go:
go
Copy
package db

import (
    "context"
    "github.com/ClickHouse/clickhouse-go/v2"
    "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

var ClickHouse driver.Conn

func InitClickHouse(addr string) error {
    var err error
    ClickHouse, err = clickhouse.Open(&clickhouse.Options{
        Addr: []string{addr},
        Auth: clickhouse.Auth{
            Database: "default",
        },
        Settings: clickhouse.Settings{
            "async_insert":          "1",
            "wait_for_async_insert": "0",
            "async_insert_busy_timeout_ms": "1000",
        },
        MaxOpenConns: 10,
        MaxIdleConns: 5,
    })
    return err
}
Validation: db.ClickHouse.Ping(context.Background()) returns nil.
Task 4: Dual-Write Implementation
Modify api/internal/handlers/upload.go batch processing (line 566 area):
go
Copy
// Replace the mutex-heavy provider counting with async ClickHouse insert
func insertToClickHouse(batch []dbRecord) error {
    if len(batch) == 0 {
        return nil
    }
    
    ctx := context.Background()
    batchReq, err := db.ClickHouse.PrepareBatch(ctx, `
        INSERT INTO reverse_ns_analytics (domain, nameserver, provider, bucket)
    `)
    if err != nil {
        return err
    }
    
    for _, r := range batch {
        provider := extractProviderName(r.ns)
        if err := batchReq.Append(r.domain, r.ns, provider, r.bucket); err != nil {
            return err
        }
    }
    
    return batchReq.Send()  // Async due to connection settings
}
Call this in your existing batch loop without mutex locking:
go
Copy
// OLD CODE (remove mutex):
// aggState.mutex.Lock()
// ... counting logic ...
// aggState.mutex.Unlock()

// NEW CODE (async, no lock):
if err := insertToClickHouse(batch); err != nil {
    log.Printf("ClickHouse insert error: %v", err)
    // Don't fail the upload, just log
}
Validation: Upload a CSV, check SELECT count() FROM reverse_ns_analytics increases.
Task 5: Replace Bubble Sort Endpoint
Replace api/internal/handlers/hosting_providers.go lines 157-163:
OLD (O(n²) bubble sort):
go
Copy
for i := 0; i < len(providers); i++ {
    for j := i + 1; j < len(providers); j++ {
        if providers[j].DomainCount > providers[i].DomainCount {
            providers[i], providers[j] = providers[j], providers[i]
        }
    }
}
NEW (ClickHouse):
go
Copy
func GetProviderStats(c *gin.Context) {
    ctx := context.Background()
    rows, err := db.ClickHouse.Query(ctx, `
        SELECT provider, domain_count, unique_nameservers 
        FROM provider_stats_mv 
        ORDER BY domain_count DESC
    `)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    defer rows.Close()
    
    var providers []ProviderStat
    for rows.Next() {
        var p ProviderStat
        if err := rows.Scan(&p.Name, &p.DomainCount, &p.NSCount); err != nil {
            continue
        }
        providers = append(providers, p)
    }
    
    c.JSON(200, providers)
}
Validation: curl /api/providers returns sorted results in <100ms regardless of data size.
Task 6: Add Query Routing Middleware
Create api/internal/handlers/router.go:
go
Copy
package handlers

import (
    "context"
    "strings"
)

// QueryRouter decides which database to use
func GetNameserverDomains(ctx context.Context, ns string, page, limit int) ([]string, error) {
    // Check if this is a "large" nameserver (use ClickHouse)
    count, _ := getDomainCountFromClickHouse(ns)
    
    if count > 10000 {
        // Large dataset: Use ClickHouse with pre-computed index
        return getFromClickHouse(ctx, ns, page, limit)
    }
    
    // Small dataset: Use ScyllaDB (faster for point lookups)
    return getFromScyllaDB(ctx, ns, page, limit)
}

func getDomainCountFromClickHouse(ns string) (int64, error) {
    var count uint64
    err := db.ClickHouse.QueryRow(context.Background(), `
        SELECT count() FROM reverse_ns_analytics WHERE nameserver = ?
    `, ns).Scan(&count)
    return int64(count), err
}
Update reverse_ns.go line 61 area to use QueryRouter instead of direct ScyllaDB calls.
Validation: Query a small NS (sub-100ms via ScyllaDB), query a large NS (sub-200ms via ClickHouse, no 100-goroutine spawn).
Task 7: Fix Memory Leak (Remove uploadStatuses Map)
In api/internal/handlers/upload.go lines 44-46, delete:
go
Copy
// DELETE THIS ENTIRE BLOCK:
var (
    uploadStatuses = make(map[string]*UploadStatus)
    uploadFailures = make(map[string][]FailedRow)
    uploadMutex    sync.RWMutex
)
Replace status tracking with ClickHouse system table or simple Redis TTL:
go
Copy
// Simple Redis-based status (auto-expires)
func setUploadStatus(filename string, status *UploadStatus) {
    ctx := context.Background()
    data, _ := json.Marshal(status)
    cache.Client.Set(ctx, "upload:"+filename, data, 10*time.Minute)
}

func getUploadStatus(filename string) *UploadStatus {
    ctx := context.Background()
    data, err := cache.Client.Get(ctx, "upload:"+filename).Result()
    if err != nil {
        return nil
    }
    var status UploadStatus
    json.Unmarshal([]byte(data), &status)
    return &status
}
Validation: Upload large file, verify memory returns to baseline after completion (no growth in uploadStatuses).
Task 8: Health Check Integration
Add to api/cmd/server/main.go health checks:
go
Copy
r.GET("/health/ready", func(c *gin.Context) {
    // Check ScyllaDB
    if db.Session == nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "reason": "scylla"})
        return
    }
    
    // Check ClickHouse
    if err := db.ClickHouse.Ping(c.Request.Context()); err != nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "reason": "clickhouse"})
        return
    }
    
    // Check Redis
    if cache.Client == nil {
        c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "reason": "redis"})
        return
    }
    
    c.JSON(http.StatusOK, gin.H{"status": "ok"})
})
Validation: curl /health/ready returns 200 with all three services up, 503 if any down.
Task 9: Backfill Historical Data
One-time migration script (run once):
go
Copy
package main

import (
    "context"
    "fmt"
    "github.com/gocql/gocql"
)

func migrateToClickHouse() {
    ctx := context.Background()
    
    // Stream from ScyllaDB
    iter := db.Session.Query("SELECT domain, nameserver, provider, bucket FROM reverse_ns").Iter()
    
    batch := make([]dbRecord, 0, 10000)
    count := 0
    
    var r dbRecord
    for iter.Scan(&r.domain, &r.ns, &r.provider, &r.bucket) {
        batch = append(batch, r)
        
        if len(batch) >= 10000 {
            insertToClickHouse(batch)
            count += len(batch)
            fmt.Printf("Migrated %d records\n", count)
            batch = batch[:0]
        }
    }
    
    // Final batch
    if len(batch) > 0 {
        insertToClickHouse(batch)
    }
}
Validation: SELECT count() FROM reverse_ns_analytics matches ScyllaDB row count.
Task 10: Configuration Update
Add to environment/config:
bash
Copy
# ScyllaDB (existing)
SCYLLA_HOSTS=127.0.0.1
SCYLLA_KEYSPACE=revns

# ClickHouse (new)
CLICKHOUSE_ADDR=127.0.0.1:9000
CLICKHOUSE_DATABASE=default

# Toggle for fallback
USE_CLICKHOUSE_ANALYTICS=true
USE_CLICKHOUSE_LARGE_NS=true
Update main.go initialization:
go
Copy
func main() {
    // Existing
    db.InitScyllaDB(os.Getenv("SCYLLA_HOSTS"))
    
    // New
    if os.Getenv("USE_CLICKHOUSE_ANALYTICS") == "true" {
        if err := db.InitClickHouse(os.Getenv("CLICKHOUSE_ADDR")); err != nil {
            log.Fatal("ClickHouse init failed:", err)
        }
    }
}
Validation: App starts successfully with both connections, fails fast if ClickHouse unreachable when enabled.