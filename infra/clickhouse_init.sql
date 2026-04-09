-- ClickHouse database schema for analytics queries
-- Optimized for fast lookups and aggregations

CREATE DATABASE IF NOT EXISTS domain_analytics;

USE domain_analytics;

-- Main table for reverse NS queries
-- Optimized for fast domain lookups by nameserver
CREATE TABLE IF NOT EXISTS reverse_ns (
    ns String,
    domain String,
    rank Int32,
    bucket Int8 DEFAULT 0,
    provider String DEFAULT '',
    added_at DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree() 
ORDER BY (ns, domain) -- Fast lookups by NS, then domain within NS
PARTITION BY toYYYYMM(added_at)
SETTINGS index_granularity = 8192;

-- Materialized view for fast NS statistics
CREATE MATERIALIZED VIEW IF NOT EXISTS ns_stats_mv
ENGINE = SummingMergeTree()
ORDER BY ns
POPULATE
AS SELECT 
    ns,
    count() as count,
    avg(rank) as avg_rank,
    uniqExact(domain) as unique_domains
FROM reverse_ns 
GROUP BY ns;

-- Materialized view for provider statistics  
CREATE MATERIALIZED VIEW IF NOT EXISTS provider_stats_mv
ENGINE = SummingMergeTree()
ORDER BY provider
POPULATE
AS SELECT 
    provider,
    count() as total_domains,
    uniqExact(ns) as unique_ns,
    avg(rank) as avg_rank
FROM reverse_ns 
WHERE provider != ''
GROUP BY provider;

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