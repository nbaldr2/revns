#!/bin/bash
# Script to populate provider_stats from reverse_ns data

echo "Populating provider_stats table from reverse_ns data..."

# Get all unique nameservers and their domain counts
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;

-- Example: Manually insert aggregated data
-- This is a workaround since ScyllaDB doesn't support GROUP BY in the same way

-- For now, let's just check what data we have
SELECT ns, COUNT(*) as domain_count FROM reverse_ns GROUP BY ns LIMIT 10;
" 2>&1 | tail -20

echo ""
echo "Note: The provider_stats table needs to be populated during data ingestion."
echo "The current reverse_ns data was uploaded via the web upload, which doesn't populate provider_stats."
echo ""
echo "Solutions:"
echo "1. Re-ingest data using the ingester tool: make ingest"
echo "2. Or use the web frontend to upload CSV again (it will populate reverse_ns)"
echo ""
echo "The hosting providers endpoint will work once provider_stats is populated."
