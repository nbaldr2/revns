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
