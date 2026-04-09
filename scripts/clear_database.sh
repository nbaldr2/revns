#!/bin/bash

# Script to clear all data from ScyllaDB tables

echo "🗑️  Clearing ScyllaDB database tables..."

# Execute TRUNCATE commands via cqlsh
docker exec -i scylla-node1 cqlsh -e "USE domain_data; TRUNCATE reverse_ns; TRUNCATE domain_metadata; TRUNCATE provider_stats; TRUNCATE provider_ns; TRUNCATE provider_domains; TRUNCATE ns_stats; TRUNCATE ingestion_progress;" 2>&1

if [ $? -eq 0 ]; then
    echo "✅ Database cleared successfully!"
    echo ""
    echo "All tables truncated:"
    echo "  - reverse_ns"
    echo "  - domain_metadata"
    echo "  - provider_stats"
    echo "  - provider_ns"
    echo "  - provider_domains"
    echo "  - ns_stats"
    echo "  - ingestion_progress"
else
    echo "❌ Error clearing database. Make sure ScyllaDB is running."
    exit 1
fi
