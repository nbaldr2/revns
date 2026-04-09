#!/bin/bash
# Backfill provider_stats and provider_ns tables from reverse_ns data

set -e

echo "🔄 Backfilling provider_stats/provider_ns from reverse_ns..."

# Ensure scylla is reachable
docker exec -i scylla-node1 cqlsh -e "USE domain_data;" >/dev/null

# Create temporary table for aggregation (if not exists)
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
CREATE TABLE IF NOT EXISTS tmp_provider_ns (
  provider text,
  ns text,
  domain_count bigint,
  PRIMARY KEY (provider, ns)
);
" >/dev/null

# Clear previous aggregated data
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
TRUNCATE provider_stats;
TRUNCATE provider_ns;
TRUNCATE tmp_provider_ns;
" >/dev/null

# Export reverse_ns to CSV, aggregate with awk, and bulk insert
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
COPY reverse_ns (ns, domain) TO STDOUT WITH HEADER = false;
" | \
awk -F',' '{
  ns=$1; gsub(/"/,"",ns);
  if (ns != "") {
    provider=tolower(ns);
    if (provider ~ /cloudflare/) provider="Cloudflare";
    else if (provider ~ /awsdns|amazon|aws/) provider="AWS";
    else if (provider ~ /godaddy/) provider="GoDaddy";
    else if (provider ~ /bluehost/) provider="Bluehost";
    else if (provider ~ /digitalocean/) provider="DigitalOcean";
    else if (provider ~ /linode/) provider="Linode";
    else if (provider ~ /vultr/) provider="Vultr";
    else if (provider ~ /google/) provider="Google";
    else if (provider ~ /namecheap/) provider="Namecheap";
    else if (provider ~ /hostgator/) provider="HostGator";
    else if (provider ~ /siteground/) provider="SiteGround";
    else if (provider ~ /hostinger/) provider="Hostinger";
    else if (provider ~ /rackspace/) provider="Rackspace";
    else if (provider ~ /cloudways/) provider="Cloudways";
    else if (provider ~ /kinsta/) provider="Kinsta";
    else if (provider ~ /wpengine/) provider="WP Engine";
    else if (provider ~ /vercel/) provider="Vercel";
    else if (provider ~ /netlify/) provider="Netlify";
    else if (provider ~ /heroku/) provider="Heroku";
    else if (provider ~ /fastly/) provider="Fastly";
    else if (provider ~ /googlehosted/) provider="Google";
    else {
      split(ns, parts, ".");
      provider=parts[1];
      if (length(provider) > 1) {
        provider=toupper(substr(provider,1,1)) substr(provider,2);
      }
    }
    key=provider"|"ns;
    counts[key]++;
    total[provider]++;
  }
}
END {
  for (k in counts) {
    split(k, parts, "|");
    printf "%s,%s,%d\n", parts[1], parts[2], counts[k];
  }
}
' | \
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
COPY tmp_provider_ns (provider, ns, domain_count) FROM STDIN WITH HEADER = false;
" >/dev/null

# Populate provider_ns from tmp table
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
INSERT INTO provider_ns (provider, ns, domain_count)
SELECT provider, ns, domain_count FROM tmp_provider_ns;
" >/dev/null

# Populate provider_stats from tmp table aggregation
docker exec -i scylla-node1 cqlsh -e "
USE domain_data;
INSERT INTO provider_stats (provider, domain_count, ns_count, total_rows, updated_at)
SELECT provider, sum(domain_count), count(ns), sum(domain_count), toTimestamp(now())
FROM tmp_provider_ns
GROUP BY provider;
" >/dev/null

echo "✅ Backfill complete."