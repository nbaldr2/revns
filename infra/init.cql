CREATE KEYSPACE IF NOT EXISTS domain_data 
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE domain_data;

-- Main table: domain -> nameserver mappings
-- SHARDED VERSION: Using bucket to prevent huge partitions for popular NS
CREATE TABLE IF NOT EXISTS reverse_ns_sharded (
    ns text,
    bucket int,
    domain text,
    rank int,
    PRIMARY KEY ((ns, bucket), domain)
) WITH CLUSTERING ORDER BY (domain ASC)
  AND compaction = {'class': 'SizeTieredCompactionStrategy'};

-- Legacy table kept for migration purposes
CREATE TABLE IF NOT EXISTS reverse_ns (
    ns text,
    domain text,
    rank int,
    PRIMARY KEY (ns, domain)
) WITH CLUSTERING ORDER BY (domain ASC);

-- Domain metadata table
CREATE TABLE IF NOT EXISTS domain_metadata (
    domain text PRIMARY KEY,
    ns_list list<text>,
    ip_list list<text>,
    rank int,
    created_at timestamp
);

-- ============================================
-- PERFORMANCE OPTIMIZATION TABLES
-- For fast ingestion and queries at scale
-- ============================================

-- Pre-aggregated provider statistics
-- Updated during ingestion for O(1) provider lookups
CREATE TABLE IF NOT EXISTS provider_stats (
    provider text PRIMARY KEY,
    domain_count bigint,
    ns_count bigint,
    total_rows bigint,
    updated_at timestamp
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Provider -> Nameserver mapping with counts
-- Allows fast breakdown of which NS belong to which provider
CREATE TABLE IF NOT EXISTS provider_ns (
    provider text,
    ns text,
    domain_count bigint,
    PRIMARY KEY (provider, ns)
) WITH CLUSTERING ORDER BY (ns ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Provider -> Domain list (for efficient provider domain queries)
CREATE TABLE IF NOT EXISTS provider_domains (
    provider text,
    domain text,
    ns text,
    rank int,
    PRIMARY KEY (provider, domain)
) WITH CLUSTERING ORDER BY (domain ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Nameserver statistics (for quick NS lookups)
CREATE TABLE IF NOT EXISTS ns_stats (
    ns text PRIMARY KEY,
    domain_count bigint,
    provider text,
    updated_at timestamp
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Ingestion batch tracking (for resume capability)
CREATE TABLE IF NOT EXISTS ingestion_progress (
    batch_id text PRIMARY KEY,
    processed_rows bigint,
    last_position bigint,
    status text,
    started_at timestamp,
    completed_at timestamp
);

-- ============================================
-- PERFORMANCE INDEXES
-- ============================================

-- Secondary index for provider lookup in provider_ns (reverse queries)
-- Removed because provider is the partition key, so an index is invalid and unnecessary

-- Secondary index for domain lookup in provider_domains
CREATE INDEX IF NOT EXISTS idx_provider_domains_ns ON provider_domains (ns);

-- Materialized view for reverse lookup: domain -> ns (for finding which NS serves a domain)
CREATE MATERIALIZED VIEW IF NOT EXISTS domain_to_ns_mv AS
SELECT domain, ns, bucket
FROM reverse_ns_sharded
WHERE domain IS NOT NULL AND ns IS NOT NULL AND bucket IS NOT NULL
PRIMARY KEY (domain, ns, bucket)
WITH CLUSTERING ORDER BY (ns ASC);

-- Materialized view for daily domain counts (for analytics)
CREATE TABLE IF NOT EXISTS daily_domain_counts (
    date date,
    provider text,
    domain_count bigint,
    PRIMARY KEY (date, provider)
) WITH CLUSTERING ORDER BY (provider ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Analytics summary table (pre-aggregated for fast dashboard queries)
CREATE TABLE IF NOT EXISTS analytics_summary (
    metric text,
    period text,
    value bigint,
    updated_at timestamp,
    PRIMARY KEY (metric, period)
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;
