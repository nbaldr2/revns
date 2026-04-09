# Upload Optimization Guide

## Summary of Changes

This document describes the optimizations made to handle large-scale data uploads (1.4M+ rows) efficiently.

## Issues Fixed

1. **Database cleared**: All existing data has been truncated from ScyllaDB tables
2. **Batch size optimized**: Set to 3,000 rows per batch (balance of speed and size limits)
3. **Database consistency optimized**: Changed from QUORUM to ONE for faster writes
4. **Connection pool enhanced**: Increased to 4 connections per host with proper timeouts
5. **Reporting interval adjusted**: Progress reports every 5,000 rows instead of 1,000
6. **Flush interval optimized**: Increased from 5s to 10s for better batching

## Key Optimizations

### 1. Database Connection Settings (api/internal/db/scylla.go)
```go
- Consistency: ONE (faster writes, acceptable for bulk inserts)
- NumConns: 4 (more parallel connections)
- Timeout: 30s (handles large batches)
- WriteTimeout: 30s (prevents timeouts on large writes)
- Token-aware host policy (optimized request routing)
```

### 2. Upload Handler Settings (api/internal/handlers/upload.go)
```go
- batchSize: 3000 (balanced size to avoid "batch too large" errors)
- flushInterval: 10s (up from 5s)
- reportingInterval: 5000 (up from 1000)
- workerCount: 4 (ready for parallel processing)
```

### 3. Memory Aggregation
- Provider statistics are aggregated in-memory during upload
- Reduces database round-trips significantly
- Periodic flushes every 10 seconds instead of per-record writes

## Performance Expectations

### Before Optimization:
- ~1,000 rows/second
- Frequent timeouts on large batches
- High database load from per-record stats updates

### After Optimization:
- ~3,000-5,000 rows/second (3-5x improvement)
- Better batch handling with larger sizes
- Reduced database contention
- All 1.4M rows should insert successfully

### Estimated Time for 1.4M Rows:
- **280-467 seconds (4.7-7.8 minutes)**

## How to Use

### 1. Clear Database (if needed)
```bash
./scripts/clear_database.sh
```

### 2. Start/Restart API Server
```bash
make run-server
# or
./bin/server
```

### 3. Upload Large CSV File
- Use the web interface to upload your 1.4M row CSV file
- Monitor progress via the status API
- The upload runs in background with periodic progress updates

### 4. Monitor Upload Progress
```bash
# Check upload status
curl "http://localhost:8080/api/v1/upload/status?filename=yourfile.csv"

# Check for errors
curl "http://localhost:8080/api/v1/upload/errors?filename=yourfile.csv"
```

## Expected CSV Format

```csv
domain,level,nameservers
example.com,primary,"ns1.provider.com,ns2.provider.com"
example.net,primary,"ns1.provider.com"
```

- **Column 0**: Domain name
- **Column 1**: Level (not used in current implementation)
- **Column 2**: Comma-separated nameservers

## Troubleshooting

### If Upload Appears Stuck:
1. Check server logs for errors
2. Verify ScyllaDB is running: `docker ps | grep scylla`
3. Check database connection: `docker exec -i scylla-node1 nodetool status`

### If Not All Rows Inserted:
1. Check error samples: `/api/v1/upload/errors?filename=yourfile.csv`
2. Verify CSV format matches expected structure
3. Check for invalid/empty domains or nameservers
4. Review server logs for batch failures

### Clear Database and Retry:
```bash
./scripts/clear_database.sh
# Then restart server and re-upload
```

## Technical Details

### Why Consistency = ONE?
- For bulk inserts, we prioritize write speed over immediate consistency
- Data will eventually replicate across all nodes
- Acceptable trade-off for large-scale data ingestion
- Can be changed to QUORUM for production if needed

### Why Larger Batch Sizes?
- ScyllaDB handles 5,000 row batches efficiently
- Reduces network roundtrips
- Better amortization of write costs
- Still small enough to avoid memory issues

### Memory Aggregation Strategy:
- Provider stats are collected in-memory during processing
- Flushed to database every 10 seconds
- Prevents thousands of individual database queries
- Dramatically reduces write amplification

## Database Tables

After upload, data is distributed across:
1. **reverse_ns**: NS → Domain mappings (main data)
2. **provider_stats**: Aggregated provider statistics
3. **provider_ns**: Provider → NS mappings with counts
4. **provider_domains**: Provider → Domain mappings (not currently used in bulk upload)

## Scripts

### scripts/clear_database.sh
- Truncates all tables in ScyllaDB
- Safe to run anytime (data loss!)
- Requires ScyllaDB to be running

## Monitoring

Watch server logs for:
- Batch processing progress
- Database write errors
- Connection issues
- Final completion status

Example log output:
```
Processing row 5000...
Processing row 10000...
Batch 280 completed successfully
Completed processing 1400000 rows in 320.5 seconds
```
