-- ClickHouse database schema for analytics queries
-- Optimized for fast lookups and aggregations
-- Auto-cleanup with TTL for data retention

CREATE DATABASE IF NOT EXISTS domain_analytics;

USE domain_analytics;

-- Main table for reverse NS queries with TTL
CREATE TABLE IF NOT EXISTS reverse_ns (
    ns String,
    domain String,
    rank Int32,
    bucket Int8 DEFAULT 0,
    provider String DEFAULT '',
    added_at DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree() 
ORDER BY (ns, domain)
PARTITION BY toYYYYMM(added_at)
TTL added_at + INTERVAL 90 DAY  -- Auto-delete after 90 days
SETTINGS index_granularity = 8192;

-- Materialized view for fast NS statistics
CREATE MATERIALIZED VIEW IF NOT EXISTS ns_stats_mv
ENGINE = SummingMergeTree()
ORDER BY ns
TTL added_at + INTERVAL 30 DAY
POPULATE
AS SELECT 
    ns,
    count() as count,
    avg(rank) as avg_rank,
    uniqExact(domain) as unique_domains,
    now() as added_at
FROM reverse_ns 
GROUP BY ns;

-- Materialized view for provider statistics  
CREATE MATERIALIZED VIEW IF NOT EXISTS provider_stats_mv
ENGINE = SummingMergeTree()
ORDER BY provider
TTL added_at + INTERVAL 30 DAY
POPULATE
AS SELECT 
    provider,
    count() as total_domains,
    uniqExact(ns) as unique_ns,
    avg(rank) as avg_rank,
    now() as added_at
FROM reverse_ns 
WHERE provider != ''
GROUP BY provider;

-- Nameserver index for fast lookups (alternative query path)
CREATE TABLE IF NOT EXISTS nameserver_domains (
    ns LowCardinality(String),
    domain String,
    provider String DEFAULT '',
    added_at DateTime DEFAULT now()
) ENGINE = MergeTree()
ORDER BY (ns, domain)
TTL added_at + INTERVAL 30 DAY;

-- Distributed table for clustering (if needed)
CREATE TABLE IF NOT EXISTS reverse_ns_distributed AS reverse_ns
ENGINE = Distributed(cluster, domain_analytics, reverse_ns, rand());

-- View for top nameservers by domain count
CREATE VIEW IF NOT EXISTS top_nameservers AS
SELECT 
    ns,
    count() as domain_count,
    avg(rank) as avg_rank
FROM reverse_ns
GROUP BY ns
ORDER BY domain_count DESC;

-- View for provider breakdown
CREATE VIEW IF NOT EXISTS provider_breakdown AS
SELECT 
    provider,
    count() as domain_count,
    uniqExact(ns) as ns_count
FROM reverse_ns 
WHERE provider != ''
GROUP BY provider
ORDER BY domain_count DESC;
