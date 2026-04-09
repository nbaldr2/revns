package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/soufianerochdi/revns-api/internal/db"
)

const (
	statsBatchSize = 500
)

type providerAgg struct {
	domainCount int64
	nsSet       map[string]struct{}
}

func main() {
	var (
		scyllaHosts = flag.String("scylla", "127.0.0.1", "Comma-separated ScyllaDB hosts")
		keyspace   = flag.String("keyspace", "domain_data", "ScyllaDB keyspace")
	)
	flag.Parse()

	ctx := context.Background()
	hosts := strings.Split(*scyllaHosts, ",")

	if err := db.Initialize(ctx, hosts, *keyspace); err != nil {
		log.Fatalf("Failed to connect to ScyllaDB: %v", err)
	}
	defer db.Close()

	log.Println("Truncating provider_stats/provider_ns...")
	if err := db.Session.Query("TRUNCATE provider_stats").Exec(); err != nil {
		log.Fatalf("Failed to truncate provider_stats: %v", err)
	}
	if err := db.Session.Query("TRUNCATE provider_ns").Exec(); err != nil {
		log.Fatalf("Failed to truncate provider_ns: %v", err)
	}

	log.Println("Scanning reverse_ns to rebuild provider aggregates...")
	providerCounts := make(map[string]*providerAgg)
	providerNSCounts := make(map[string]map[string]int64)

	iter := db.Session.Query("SELECT ns FROM reverse_ns").Iter()
	var ns string
	for iter.Scan(&ns) {
		provider := extractProviderName(ns)
		if providerCounts[provider] == nil {
			providerCounts[provider] = &providerAgg{nsSet: make(map[string]struct{})}
		}
		providerCounts[provider].domainCount++
		providerCounts[provider].nsSet[ns] = struct{}{}

		if providerNSCounts[provider] == nil {
			providerNSCounts[provider] = make(map[string]int64)
		}
		providerNSCounts[provider][ns]++
	}
	if err := iter.Close(); err != nil {
		log.Fatalf("Failed scanning reverse_ns: %v", err)
	}

	log.Println("Writing provider_stats...")
	writeProviderStats(providerCounts)

	log.Println("Writing provider_ns...")
	writeProviderNS(providerNSCounts)

	log.Printf("Backfill complete: %d providers", len(providerCounts))
}

func writeProviderStats(providerCounts map[string]*providerAgg) {
	batch := db.Session.NewBatch(gocql.UnloggedBatch)
	count := 0
	now := time.Now()
	for provider, agg := range providerCounts {
		batch.Query(
			"INSERT INTO provider_stats (provider, domain_count, ns_count, total_rows, updated_at) VALUES (?, ?, ?, ?, ?)",
			provider, agg.domainCount, int64(len(agg.nsSet)), agg.domainCount, now,
		)
		count++
		if count >= statsBatchSize {
			if err := db.Session.ExecuteBatch(batch); err != nil {
				log.Fatalf("Failed inserting provider_stats batch: %v", err)
			}
			batch = db.Session.NewBatch(gocql.UnloggedBatch)
			count = 0
		}
	}
	if count > 0 {
		if err := db.Session.ExecuteBatch(batch); err != nil {
			log.Fatalf("Failed inserting provider_stats batch: %v", err)
		}
	}
}

func writeProviderNS(providerNSCounts map[string]map[string]int64) {
	batch := db.Session.NewBatch(gocql.UnloggedBatch)
	count := 0
	for provider, nsCounts := range providerNSCounts {
		for ns, domainCount := range nsCounts {
			batch.Query(
				"INSERT INTO provider_ns (provider, ns, domain_count) VALUES (?, ?, ?)",
				provider, ns, domainCount,
			)
			count++
			if count >= statsBatchSize {
				if err := db.Session.ExecuteBatch(batch); err != nil {
					log.Fatalf("Failed inserting provider_ns batch: %v", err)
				}
				batch = db.Session.NewBatch(gocql.UnloggedBatch)
				count = 0
			}
		}
	}
	if count > 0 {
		if err := db.Session.ExecuteBatch(batch); err != nil {
			log.Fatalf("Failed inserting provider_ns batch: %v", err)
		}
	}
}

// extractProviderName extracts provider name from nameserver
func extractProviderName(ns string) string {
	ns = strings.ToLower(ns)
	providers := map[string]string{
		"cloudflare":   "Cloudflare",
		"awsdns":       "AWS",
		"aws":          "AWS",
		"amazon":       "AWS",
		"hostgator":    "HostGator",
		"godaddy":      "GoDaddy",
		"bluehost":     "Bluehost",
		"digitalocean": "DigitalOcean",
		"linode":       "Linode",
		"vultr":        "Vultr",
		"google":       "Google",
		"googleapis":   "Google",
		"nsone":        "NS1",
		"alidns":       "Alibaba Cloud",
		"hichina":      "Alibaba Cloud",
		"namecheap":    "Namecheap",
		"registrar-servers": "Namecheap",
		"dreamhost":    "DreamHost",
		"siteground":   "SiteGround",
		"hostinger":    "Hostinger",
		"rackspace":    "Rackspace",
		"cloudways":    "Cloudways",
		"kinsta":       "Kinsta",
		"wpengine":     "WP Engine",
		"vercel":       "Vercel",
		"netlify":      "Netlify",
		"heroku":       "Heroku",
		"fastly":       "Fastly",
		"googlehosted": "Google",
		"ultradns":     "UltraDNS",
		"domaincontrol": "GoDaddy",
		"ovh":          "OVH",
	}
	for keyword, provider := range providers {
		if strings.Contains(ns, keyword) {
			return provider
		}
	}
	parts := strings.Split(ns, ".")
	for i := 0; i < len(parts); i++ {
		label := parts[i]
		if label == "" || len(label) <= 2 {
			continue
		}
		if isGenericNSLabel(label) {
			continue
		}
		return strings.ToUpper(label[:1]) + label[1:]
	}
	return "Unknown"
}

func isGenericNSLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return false
	}
	if label == "ns" || label == "dns" || label == "pdns" {
		return true
	}
	prefixLen := 0
	if strings.HasPrefix(label, "pdns") {
		prefixLen = 4
	} else if strings.HasPrefix(label, "dns") {
		prefixLen = 3
	} else if strings.HasPrefix(label, "ns") {
		prefixLen = 2
	}
	if prefixLen > 0 && len(label) > prefixLen {
		for i := prefixLen; i < len(label); i++ {
			if label[i] < '0' || label[i] > '9' {
				return false
			}
		}
		return true
	}
	letters := 0
	digits := 0
	digitSeen := false
	for i := 0; i < len(label); i++ {
		ch := label[i]
		if ch >= '0' && ch <= '9' {
			digits++
			digitSeen = true
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			if digitSeen {
				return false
			}
			letters++
			continue
		}
		return false
	}
	if digits > 0 && letters > 0 && letters <= 3 {
		return true
	}
	return false
}