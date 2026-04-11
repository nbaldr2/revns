# Performance Optimization Guide for REVNS

This guide explains how to make REVNS faster at fetching data from the database.

## Current Architecture Performance Features

### 1. Hybrid Database Architecture

**ScyllaDB (Operational)** - For real-time queries
- Sharded table design (`reverse_ns_sharded`) with 100 buckets
- Even distribution of data across nodes
- Low-latency point queries

**ClickHouse (Analytics)** - For large aggregations
- Columnar storage for fast analytical queries
- Async inserts for high-throughput writes
- Automatic query routing based on data size

### 2. Caching Strategy

**Redis Caching**
- Hosting providers list cached with 5-minute TTL
- Circuit breaker pattern prevents cache stampedes
- Cache invalidation on data updates

**Singleflight Pattern**
- Prevents duplicate in-flight requests
- Reduces database load during traffic spikes

## Optimization Techniques

### 1. Database Query Optimizations

#### A. Indexing Strategy
```sql
-- Primary keys are already optimized in ScyllaDB
-- For reverse_ns table:
PRIMARY KEY (ns, domain, rank)

-- For reverse_ns_sharded table:
PRIMARY KEY ((ns, bucket), domain, rank)
```

**Recommendations:**
- Add secondary indexes for frequently queried fields
- Use materialized views for common query patterns
- Consider SASI indexes for text search (if needed)

#### B. Query Routing
The system automatically routes queries based on data size:

```go
// QueryRouter decides which database to use
if domainCount > largeNSThreshold {
    return clickhouse.Query(...)  // For large datasets
}
return scylla.Query(...)          // For small datasets
```

**Tuning:**
- Adjust `largeNSThreshold` based on your data (default: 10,000)
- Monitor query performance to find optimal threshold

### 2. Connection Pooling

#### ScyllaDB Connection Pool
```go
cluster := scylladb.NewCluster(hosts...)
cluster.NumConns = 50              // Increase for high concurrency
cluster.Timeout = 30 * time.Second
cluster.MaxPreparedStatements = 1000
```

**Tuning:**
- Increase `NumConns` based on concurrent request volume
- Monitor connection pool metrics in Grafana

#### Redis Connection Pool
```go
redisClient := redis.NewClient(&redis.Options{
    PoolSize: 100,                  // Increase for high traffic
    MinIdleConns: 10,
    MaxRetries: 3,
})
```

### 3. Batch Operations

#### Batch Inserts (Already Implemented)
```go
// Use batch statements for bulk inserts
batch := session.NewBatch(gocql.LoggedBatch)
for _, domain := range domains {
    batch.Query(insertQuery, domain.NS, domain.Domain, domain.Rank)
}
err := session.ExecuteBatch(batch)
```

**Optimization:**
- Batch size: 100-500 rows per batch
- Use `UnloggedBatch` for better performance (if atomicity not required)

#### Paging for Large Results
```go
// Use paging for large result sets
query := session.Query("SELECT * FROM reverse_ns WHERE ns = ?", ns)
query.PageSize(1000)  // Adjust based on row size
iter := query.Iter()
```

### 4. Read Replicas

#### ScyllaDB Read Replicas
```yaml
# In ScyllaDB config
read_request_timeout_in_ms: 5000
write_request_timeout_in_ms: 2000
```

**Setup:**
1. Configure RF (Replication Factor) = 3
2. Use `LOCAL_QUORUM` for reads (already configured)
3. Distribute nodes across availability zones

### 5. Async Processing

#### Async Inserts (Already Implemented)
```go
// ClickHouse async inserts
chConfig := &clickhouse.Options{
    Settings: clickhouse.Settings{
        "async_insert": "1",
        "wait_for_async_insert": "0",
        "async_insert_max_data_size": "10000000",
    },
}
```

#### Background Jobs
- Use Redis streams for background processing
- Implement worker pools for heavy operations

### 6. Data Modeling Optimizations

#### Partition Key Design
```sql
-- Good: Even distribution
PRIMARY KEY ((ns, bucket), domain, rank)

-- Avoid: Hot partitions
PRIMARY KEY (ns, domain, rank)  -- For very large NS
```

**Tips:**
- Use composite partition keys for even distribution
- Monitor partition sizes (should be < 100MB)
- Use bucketing for high-cardinality keys

#### Denormalization
- Store pre-aggregated stats in ClickHouse
- Use materialized views for common joins
- Duplicate data for read optimization

### 7. Query Optimization

#### Prepared Statements
```go
// Prepare statements once, reuse
stmt := session.Query("SELECT * FROM reverse_ns WHERE ns = ?")
stmt.Bind(ns).Exec()
```

#### Select Only Needed Columns
```go
// Good
query := "SELECT domain FROM reverse_ns WHERE ns = ?"

// Avoid
query := "SELECT * FROM reverse_ns WHERE ns = ?"
```

#### Limit Results
```go
query := "SELECT domain FROM reverse_ns WHERE ns = ? LIMIT 10000"
```

### 8. Caching Improvements

#### Multi-Level Caching
1. **L1**: In-memory cache (singleflight)
2. **L2**: Redis cache (5 min TTL)
3. **L3**: Database

#### Cache Warming
```go
// Warm cache on startup
func warmCache() {
    providers := getTopProviders()
    redis.Set("top_providers", providers, 5*time.Minute)
}
```

### 9. Monitoring & Profiling

#### Key Metrics to Monitor
- Query latency (p50, p95, p99)
- Cache hit/miss ratio
- Connection pool utilization
- ScyllaDB node load
- ClickHouse query times

#### Tools
- Prometheus metrics (already exposed at `/metrics`)
- Grafana dashboards (http://localhost:3000)
- ScyllaDB monitoring (port 9180)

### 10. Advanced Optimizations

#### Materialized Views
```sql
-- Create MV for common queries
CREATE MATERIALIZED VIEW reverse_ns_by_rank AS
    SELECT ns, domain, rank FROM reverse_ns
    PRIMARY KEY (rank, ns, domain)
    WITH CLUSTERING ORDER BY (ns ASC);
```

#### Secondary Indexes (Use Sparingly)
```sql
-- For text search (if needed)
CREATE CUSTOM INDEX domain_sasi ON reverse_ns (domain)
USING 'org.apache.cassandra.index.sasi.SASIIndex'
WITH OPTIONS = {
    'mode': 'CONTAINS',
    'analyzer_class': 'org.apache.cassandra.index.sasi.analyzer.StandardAnalyzer'
};
```

#### Read Repair Disabled (for performance)
```yaml
# In ScyllaDB config
read_request_timeout_in_ms: 5000
 hinted_handoff_enabled: false
 read_repair_chance: 0.0
```

## Quick Wins

1. **Increase Cache TTL**: Extend Redis TTL for stable data
2. **Add Connection Pooling**: Increase pool sizes based on load
3. **Use Prepared Statements**: Already implemented, ensure usage
4. **Limit Result Sets**: Always use LIMIT for large queries
5. **Enable Compression**: Between API and database
6. **Use Keep-Alive**: HTTP keep-alive for frontend connections

## Configuration Checklist

- [ ] ScyllaDB: RF=3, CL=LOCAL_QUORUM
- [ ] Redis: PoolSize >= 100
- [ ] ClickHouse: Async inserts enabled
- [ ] API: Singleflight enabled
- [ ] Queries: Prepared statements used
- [ ] Caching: TTL optimized for data volatility
- [ ] Monitoring: Grafana dashboards configured

## Load Testing

```bash
# Run k6 load tests
make load-test

# Or manual test
k6 run --vus 100 --duration 5m data/load_test.js
```

## Performance Targets

| Metric | Target |
|--------|--------|
| API Response Time (p95) | < 100ms |
| Database Query Time (p95) | < 50ms |
| Cache Hit Ratio | > 80% |
| Concurrent Users | 10,000+ |
| Ingestion Rate | 10,000 rows/sec |

## Additional Resources

- [ScyllaDB Performance Tuning](https://docs.scylladb.com/operating-scylla/administration-tuning/)
- [ClickHouse Performance](https://clickhouse.com/docs/en/operations/performance-test/)
- [Redis Optimization](https://redis.io/docs/management/optimization/)
