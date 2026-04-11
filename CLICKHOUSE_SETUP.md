# ClickHouse Setup Guide

This guide explains how to configure and verify ClickHouse integration for the REVNS analytics system.

## Overview

ClickHouse is used as an **analytical layer** for:
- Fast provider rankings and statistics
- Global analytics queries
- Trend analysis
- Bulk aggregations

The **operational layer** (ScyllaDB) handles:
- Real-time domain lookups
- Nameserver existence checks
- Individual record CRUD
- Low latency point queries

## Configuration

### Environment Variables

Add these to your `.env` file:

```bash
# ClickHouse Connection
CLICKHOUSE_ADDR=127.0.0.1:9000
CLICKHOUSE_USER=default
CLICKHOUSE_PASSWORD=

# Feature Flags
USE_CLICKHOUSE_ANALYTICS=true    # Enable/disable ClickHouse analytics
USE_CLICKHOUSE_LARGE_NS=true      # Use ClickHouse for large nameserver queries (>10k domains)
```

### Docker Setup (Development)

```bash
# Run ClickHouse container
docker run -d --name clickhouse-revns \
  --ulimit nofile=262144:262144 \
  -p 8123:8123 \
  -p 9000:9000 \
  -p 9009:9009 \
  -v clickhouse_data:/var/lib/clickhouse \
  clickhouse/clickhouse-server:latest

# Default credentials (no password for 'default' user)
# HTTP Interface: http://localhost:8123
# Native Interface: localhost:9000
```

### Production Setup

```bash
# Using Docker Compose (see infra/docker-compose.yml)
docker-compose up -d clickhouse

# Or Helm chart for Kubernetes
helm install clickhouse bitnami/clickhouse -f values.yaml
```

## Database Schema

Initialize the schema by running `infra/clickhouse_init.sql`:

```bash
# Connect to ClickHouse and execute schema
clickhouse-client --host 127.0.0.1 --port 9000 --user default < infra/clickhouse_init.sql

# Or via HTTP
curl -X POST 'http://127.0.0.1:8123' --data-binary @infra/clickhouse_init.sql
```

### Schema Overview

| Table/View | Purpose | TTL |
|------------|---------|-----|
| `reverse_ns` | Main domain mappings | 90 days |
| `ns_stats_mv` | Nameserver statistics (materialized) | 30 days |
| `provider_stats_mv` | Provider statistics (materialized) | 30 days |
| `nameserver_domains` | Fast NS lookups | 30 days |
| `top_nameservers` | View for top NS by domain count | - |
| `provider_breakdown` | View for provider breakdown | - |

## Verifying ClickHouse Connection

### 1. Health Check Endpoint

```bash
# Start the API server with ClickHouse enabled
USE_CLICKHOUSE_ANALYTICS=true ./server

# Check health (will include ClickHouse status if enabled)
curl http://localhost:8080/health/ready
```

Expected response when ClickHouse is connected:
```json
{
  "status": "ok",
  "checks": {
    "database": {"status": "ok"},
    "cache": {"status": "ok"}
  }
}
```

### 2. Manual Connection Test

```bash
# Using clickhouse-client
clickhouse-client --host 127.0.0.1 --port 9000

# Test basic query
SELECT 1

# Check databases
SHOW DATABASES

# Check tables
USE domain_analytics
SHOW TABLES
```

### 3. HTTP Interface Check

```bash
# Ping test
curl http://localhost:8123/ping
# Returns: Ok

# Query via HTTP
curl 'http://localhost:8123/?query=SELECT%201'
```

### 4. Go SDK Test

```go
import "github.com/soufianerochdi/revns-api/internal/analytics"

// In your code:
ctx := context.Background()
err := analytics.InitializeClickHouse(ctx, "127.0.0.1:9000")
if err != nil {
    log.Fatal("Failed:", err)
}

// Verify connection
err = analytics.Ping(ctx)
if err != nil {
    log.Fatal("ClickHouse not responding:", err)
}
log.Println("ClickHouse connected successfully!")
```

## Troubleshooting

### Connection Refused

```bash
# Check if ClickHouse is running
docker ps | grep clickhouse

# Check logs
docker logs clickhouse-revns

# Restart container
docker restart clickhouse-revns
```

### Authentication Failed

```bash
# Default ClickHouse has 'default' user with no password
# If you set a password, update your .env:

CLICKHOUSE_PASSWORD=your_secure_password
```

### Database Not Found

```bash
# Create the database manually
clickhouse-client --host 127.0.0.1 --port 9000
CREATE DATABASE IF NOT EXISTS domain_analytics;
exit

# Then run schema
clickhouse-client --host 127.0.0.1 --port 9000 < infra/clickhouse_init.sql
```

### Query Errors

```sql
-- Check table exists
SELECT name FROM system.tables WHERE database = 'domain_analytics';

-- Check data
SELECT count() FROM domain_analytics.reverse_ns;

-- Check materialized views
SELECT name FROM system.tables WHERE database = 'domain_analytics' AND engine LIKE '%Materialized%';
```

## Performance Tips

### Async Inserts (Enabled by Default)

The connection is configured with async inserts for faster batch writes:

```go
Settings: clickhouse.Settings{
    "async_insert":                 "1",
    "wait_for_async_insert":        "0",
    "async_insert_busy_timeout_ms":  "1000",
}
```

### Query Routing

Large nameserver queries (>10,000 domains) automatically route to ClickHouse:

```go
// Threshold can be adjusted in query_router.go
const LargeNSThreshold = 10000
```

### Index Optimization

ClickHouse uses `LowCardinality` for nameserver strings to reduce memory:

```sql
ns LowCardinality(String),
```

## Monitoring

### Query Performance

```sql
-- Check query execution time
SELECT 
    query,
    elapsed,
    memory_usage
FROM system.query_log
WHERE type = 'QueryFinish'
ORDER BY event_time DESC
LIMIT 10;
```

### Table Sizes

```sql
SELECT 
    database,
    table,
    formatReadableSize(sum(bytes)) AS size,
    sum(rows) AS rows
FROM system.parts
WHERE database = 'domain_analytics'
GROUP BY database, table;
```

### Resource Usage

```sql
-- Check ClickHouse resource usage
SELECT 
    metric,
    value
FROM system.metrics
WHERE metric IN ('Query', 'Merge', 'BackgroundPoolTask', 'MemoryTracking');
```

## Security

### Network Security

```bash
# Allow only specific IPs (in clickhouse.xml)
<listen_host>127.0.0.1</listen_host>
<!-- Or for specific network: -->
<!-- <listen_host>10.0.0.0/8</listen_host> -->
```

### User Permissions

```sql
-- Create read-only user for analytics
CREATE USER readonly_user IDENTIFIED BY 'password';
GRANT SELECT ON domain_analytics.* TO readonly_user;

-- Create admin user for writes
CREATE USER admin_user IDENTIFIED BY 'strong_password';
GRANT ALL ON domain_analytics.* TO admin_user;
```

## Quick Start Checklist

- [ ] Install ClickHouse (Docker or native)
- [ ] Run `infra/clickhouse_init.sql`
- [ ] Add credentials to `.env`
- [ ] Set `USE_CLICKHOUSE_ANALYTICS=true`
- [ ] Start API server
- [ ] Verify with `curl http://localhost:8080/health/ready`
- [ ] Check tables with `clickhouse-client`
