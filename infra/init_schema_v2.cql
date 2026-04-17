-- ============================================================================
-- SCHEMA V2: Per-NS Tables + Accurate Stats
-- Purpose: Create dedicated tables for each NS for O(1) domain fetching
-- Data Safety: All existing tables remain untouched
-- ============================================================================

USE domain_data;

-- ============================================================================
-- 1. DOMAIN METADATA TABLE (Populated from existing data)
-- Stores unique domain info aggregated from all sources
-- ============================================================================

CREATE TABLE IF NOT EXISTS domain_metadata_v2 (
    domain text PRIMARY KEY,
    ns_list list<text>,           -- All NS for this domain
    provider_list list<text>,     -- All providers for this domain
    first_seen timestamp,
    last_updated timestamp,
    total_occurrences bigint      -- How many times this domain appears
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- ============================================================================
-- 2. ACCURATE STATS TABLE (Computed from actual data)
-- ============================================================================

CREATE TABLE IF NOT EXISTS global_stats_v2 (
    stat_name text PRIMARY KEY,
    stat_value bigint,
    last_computed timestamp,
    computation_method text
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- Pre-populate with known values
INSERT INTO global_stats_v2 (stat_name, stat_value, last_computed, computation_method) 
VALUES ('total_domain_records', 13061436, toTimestamp(now()), 'sum_of_provider_counts');

INSERT INTO global_stats_v2 (stat_name, stat_value, last_computed, computation_method) 
VALUES ('unique_domains', 0, toTimestamp(now()), 'pending_computation');

INSERT INTO global_stats_v2 (stat_name, stat_value, last_computed, computation_method) 
VALUES ('total_providers', 1394922, toTimestamp(now()), 'count_provider_stats');

INSERT INTO global_stats_v2 (stat_name, stat_value, last_computed, computation_method) 
VALUES ('total_nameservers', 3667806, toTimestamp(now()), 'count_provider_ns');

-- ============================================================================
-- 3. PER-NS DOMAIN TABLES TEMPLATE
-- Each NS gets its own table: domains_ns_{sanitized_ns_name}
-- Table name example: domains_ns_cloudflare_com
-- ============================================================================

-- Template for creating per-NS tables
-- Will be executed programmatically for each unique NS
/*
CREATE TABLE IF NOT EXISTS domains_ns_{sanitized_name} (
    domain text PRIMARY KEY,
    rank int,
    first_seen timestamp
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;
*/

-- ============================================================================
-- 4. NS TABLE INDEX (Tracks which tables exist)
-- ============================================================================

CREATE TABLE IF NOT EXISTS ns_table_index (
    ns text PRIMARY KEY,
    table_name text,
    domain_count bigint,
    created_at timestamp,
    last_updated timestamp,
    is_populated boolean
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- ============================================================================
-- 5. MIGRATION TRACKING TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS migration_status (
    migration_id text PRIMARY KEY,
    started_at timestamp,
    completed_at timestamp,
    status text,  -- 'running', 'completed', 'failed'
    records_processed bigint,
    records_failed bigint,
    error_message text
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;

-- ============================================================================
-- 6. BACKGROUND JOB TRACKING
-- ============================================================================

CREATE TABLE IF NOT EXISTS background_jobs (
    job_id text PRIMARY KEY,
    job_type text,
    started_at timestamp,
    completed_at timestamp,
    status text,
    progress_percent int,
    last_updated timestamp,
    result text
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND gc_grace_seconds = 86400;
