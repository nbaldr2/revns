package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
)

const (
	defaultBatchSize     = 500  // Reduced from 1000 to prevent latency spikes
	defaultWorkers       = 20
	defaultFlushInterval = 5 * time.Second
	numBuckets           = 100  // Shard into 100 buckets for popular NS
)

// calculateBucket determines which shard a domain belongs to
// Uses consistent hashing to distribute domains across buckets
func calculateBucket(domain string) int {
	hash := 0
	for _, c := range domain {
		hash = (hash*31 + int(c)) % 10007 // Prime number for better distribution
	}
	return hash % numBuckets
}

type record struct {
	domain   string
	ns       string
	rank     int
	provider string
}

// Aggregation maps for real-time stats
type aggregationState struct {
	providerStats    map[string]*providerAgg
	nsStats          map[string]*nsAgg
	providerNSCounts map[string]map[string]int64 // provider -> ns -> count
	mutex            sync.RWMutex
}

type providerAgg struct {
	domainCount int64
	nsSet       map[string]struct{}
	totalRows   int64
}

type nsAgg struct {
	domainCount int64
	provider    string
}

func main() {
	var (
		csvFile     = flag.String("csv", "", "Path to CSV file (format: domain,level,ns)")
		scyllaHosts = flag.String("scylla", "127.0.0.1", "Comma-separated ScyllaDB hosts")
		redisAddr   = flag.String("redis", "127.0.0.1:6379", "Redis address")
		batchSize   = flag.Int("batch", defaultBatchSize, "Batch size for inserts")
		workers     = flag.Int("workers", defaultWorkers, "Number of parallel workers")
		warmCache   = flag.Bool("warm-cache", true, "Warm Redis cache after ingestion")
		skipClear   = flag.Bool("skip-clear", false, "Skip clearing existing data")
	)
	flag.Parse()

	if *csvFile == "" {
		log.Fatal("Please provide a CSV file path using -csv flag")
	}

	ctx := context.Background()

	hosts := strings.Split(*scyllaHosts, ",")
	if err := db.Initialize(ctx, hosts, "domain_data"); err != nil {
		log.Fatalf("Failed to connect to ScyllaDB: %v", err)
	}
	defer db.Close()

	if err := cache.Initialize(ctx, *redisAddr); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer cache.Close()

	if !*skipClear {
		log.Println("Clearing existing data...")
		clearAllTables(ctx)
		log.Println("Database cleared.")
	}

	aggState := &aggregationState{
		providerStats:    make(map[string]*providerAgg),
		nsStats:          make(map[string]*nsAgg),
		providerNSCounts: make(map[string]map[string]int64),
	}

	startTime := time.Now()
	stats := processCSV(ctx, *csvFile, *batchSize, *workers, aggState)

	log.Println("Flushing aggregated statistics...")
	flushAggregationToDB(ctx, aggState)

	log.Printf("Ingestion complete: %d rows, %d insertions, %d nameservers, %d errors in %v",
		stats.rows, stats.insertions, stats.nameservers, stats.errors, time.Since(startTime))

	if *warmCache && stats.nameservers > 0 {
		log.Println("Warming Redis cache...")
		warmStart := time.Now()
		warmCacheForTopNameservers(ctx, stats.nsCounts, 100)
		log.Printf("Cache warming complete in %v", time.Since(warmStart))
	}
}

func clearAllTables(ctx context.Context) {
	tables := []string{
		"reverse_ns",
		"provider_stats",
		"provider_ns",
		"provider_domains",
		"ns_stats",
	}

	for _, table := range tables {
		log.Printf("Truncating %s...", table)
		if err := db.Session.Query(fmt.Sprintf("TRUNCATE %s", table)).Exec(); err != nil {
			log.Printf("Warning: Failed to truncate %s: %v", table, err)
		}
	}

	if err := cache.Client.FlushDB(ctx).Err(); err != nil {
		log.Printf("Warning: Failed to flush Redis: %v", err)
	}
}

type stats struct {
	rows        int
	insertions  int
	nameservers int
	errors      int
	nsCounts    map[string]int
	nsMutex     sync.Mutex
}

func processCSV(ctx context.Context, filename string, batchSize, workers int, aggState *aggregationState) *stats {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Failed to open CSV: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReader(file))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	s := &stats{nsCounts: make(map[string]int)}

	header, err := reader.Read()
	if err != nil {
		log.Fatalf("Failed to read CSV header: %v", err)
	}
	log.Printf("CSV columns: %v", header)

	recordCh := make(chan record, batchSize*workers)
	var wg sync.WaitGroup

	flushTicker := time.NewTicker(defaultFlushInterval)
	defer flushTicker.Stop()

	go func() {
		for range flushTicker.C {
			flushAggregationToDB(ctx, aggState)
		}
	}()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(ctx, recordCh, &wg, s, batchSize, aggState)
	}

	go func() {
		defer close(recordCh)
		lineNum := 1
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			lineNum++
			if err != nil {
				log.Printf("CSV read error at line %d: %v", lineNum, err)
				s.nsMutex.Lock()
				s.errors++
				s.nsMutex.Unlock()
				continue
			}

			if len(row) < 3 {
				continue
			}

			domain := strings.TrimSpace(row[0])
			if domain == "" {
				continue
			}

			var rank int
			fmt.Sscanf(row[1], "%d", &rank)

			nsField := row[2]
			nsList := strings.Split(nsField, ",")

			for _, ns := range nsList {
				ns = strings.TrimSpace(ns)
				if ns != "" {
					provider := extractProvider(ns)
					recordCh <- record{domain: domain, ns: ns, rank: rank, provider: provider}
				}
			}

			if lineNum%100000 == 0 {
				log.Printf("Processed %d rows...", lineNum)
			}
		}
	}()

	wg.Wait()
	return s
}

func worker(ctx context.Context, recordCh <-chan record, wg *sync.WaitGroup, s *stats, batchSize int, aggState *aggregationState) {
	defer wg.Done()

	batch := make([]record, 0, batchSize)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := insertBatchUnlogged(ctx, batch); err != nil {
			log.Printf("Batch insert error: %v", err)
			s.nsMutex.Lock()
			s.errors += len(batch)
			s.nsMutex.Unlock()
		} else {
			updateAggregation(aggState, batch)

			uniqueNS := make(map[string]bool)
			for _, r := range batch {
				uniqueNS[r.ns] = true
			}
			s.nsMutex.Lock()
			s.insertions += len(batch)
			for ns := range uniqueNS {
				if s.nsCounts[ns] == 0 {
					s.nameservers++
				}
				s.nsCounts[ns]++
			}
			s.nsMutex.Unlock()
		}
		batch = batch[:0]
	}

	for {
		select {
		case r, ok := <-recordCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, r)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func insertBatchUnlogged(ctx context.Context, batch []record) error {
	if len(batch) == 0 {
		return nil
	}

	b := db.Session.NewBatch(0)
	for _, r := range batch {
		bucket := calculateBucket(r.domain) // Shard by domain for even distribution
		b.Query(
			"INSERT INTO reverse_ns_sharded (ns, bucket, domain, rank) VALUES (?, ?, ?, ?)",
			r.ns, bucket, r.domain, r.rank,
		)
		b.Query(
			"INSERT INTO provider_domains (provider, domain, ns, rank) VALUES (?, ?, ?, ?)",
			r.provider, r.domain, r.ns, r.rank,
		)
	}

	return db.Session.ExecuteBatch(b)
}

func updateAggregation(aggState *aggregationState, batch []record) {
	aggState.mutex.Lock()
	defer aggState.mutex.Unlock()

	for _, r := range batch {
		if aggState.providerStats[r.provider] == nil {
			aggState.providerStats[r.provider] = &providerAgg{
				nsSet: make(map[string]struct{}),
			}
		}
		p := aggState.providerStats[r.provider]
		p.domainCount++
		p.nsSet[r.ns] = struct{}{}
		p.totalRows++

		if aggState.nsStats[r.ns] == nil {
			aggState.nsStats[r.ns] = &nsAgg{provider: r.provider}
		}
		aggState.nsStats[r.ns].domainCount++

		if aggState.providerNSCounts[r.provider] == nil {
			aggState.providerNSCounts[r.provider] = make(map[string]int64)
		}
		aggState.providerNSCounts[r.provider][r.ns]++
	}
}

func flushAggregationToDB(ctx context.Context, aggState *aggregationState) {
	aggState.mutex.Lock()
	defer aggState.mutex.Unlock()

	now := time.Now()

	for provider, agg := range aggState.providerStats {
		err := db.Session.Query(
			"INSERT INTO provider_stats (provider, domain_count, ns_count, total_rows, updated_at) VALUES (?, ?, ?, ?, ?)",
			provider, agg.domainCount, int64(len(agg.nsSet)), agg.totalRows, now,
		).Exec()
		if err != nil {
			log.Printf("Failed to write provider_stats for %s: %v", provider, err)
		}
	}

	for ns, agg := range aggState.nsStats {
		err := db.Session.Query(
			"INSERT INTO ns_stats (ns, domain_count, provider, updated_at) VALUES (?, ?, ?, ?)",
			ns, agg.domainCount, agg.provider, now,
		).Exec()
		if err != nil {
			log.Printf("Failed to write ns_stats for %s: %v", ns, err)
		}
	}

	for provider, nsCounts := range aggState.providerNSCounts {
		for ns, count := range nsCounts {
			err := db.Session.Query(
				"INSERT INTO provider_ns (provider, ns, domain_count) VALUES (?, ?, ?)",
				provider, ns, count,
			).Exec()
			if err != nil {
				log.Printf("Failed to write provider_ns for %s/%s: %v", provider, ns, err)
			}
		}
	}

	log.Printf("Flushed aggregation: %d providers, %d nameservers",
		len(aggState.providerStats), len(aggState.nsStats))
}

func extractProvider(ns string) string {
	ns = strings.ToLower(ns)

	providers := map[string]string{
		"cloudflare":      "Cloudflare",
		"awsdns":          "AWS",
		"aws":             "AWS",
		"amazon":          "AWS",
		"hostgator":       "HostGator",
		"godaddy":         "GoDaddy",
		"bluehost":        "Bluehost",
		"digitalocean":    "DigitalOcean",
		"linode":          "Linode",
		"vultr":           "Vultr",
		"google":          "Google",
		"googleapis":      "Google",
		"namecheap":       "Namecheap",
		"dreamhost":       "DreamHost",
		"inmotionhosting": "InMotion Hosting",
		"a2hosting":       "A2 Hosting",
		"siteground":      "SiteGround",
		"hostinger":       "Hostinger",
		"ipage":           "iPage",
		"media":           "Media Temple",
		"rackspace":       "Rackspace",
		"ovh":             "OVH",
		"hetzner":         "Hetzner",
		"cloudways":       "Cloudways",
		"kinsta":          "Kinsta",
		"wpengine":        "WP Engine",
		"pantheon":        "Pantheon",
		"fly":             "Fly.io",
		"vercel":          "Vercel",
		"netlify":         "Netlify",
		"heroku":          "Heroku",
		"fastly":          "Fastly",
		"akamai":          "Akamai",
		"edgecast":        "Edgecast",
	}

	for keyword, provider := range providers {
		if strings.Contains(ns, keyword) {
			return provider
		}
	}

	parts := strings.Split(ns, ".")
	if len(parts) >= 2 {
		for i := len(parts) - 2; i >= 0; i-- {
			label := strings.TrimSpace(parts[i])
			if label == "" {
				continue
			}
			if isIgnoredProviderLabel(label) {
				continue
			}
			return strings.ToUpper(label[:1]) + label[1:]
		}
	}

	return "Unknown"
}

func isIgnoredProviderLabel(label string) bool {
	ignored := map[string]bool{
		"com": true, "net": true, "org": true, "co": true,
		"biz": true, "info": true, "io": true, "app": true,
		"site": true, "online": true, "cloud": true, "dev": true,
		"uk": true, "de": true, "fr": true, "it": true,
		"es": true, "nl": true, "pl": true, "ru": true, "eu": true,
	}

	if ignored[label] {
		return true
	}

	if strings.HasPrefix(label, "ns") || strings.HasPrefix(label, "dns") || strings.HasPrefix(label, "pdns") {
		return true
	}

	if len(label) <= 2 {
		return true
	}

	return false
}

func warmCacheForTopNameservers(ctx context.Context, nsCounts map[string]int, topN int) {
	type nsCount struct {
		ns    string
		count int
	}

	var counts []nsCount
	for ns, count := range nsCounts {
		counts = append(counts, nsCount{ns, count})
	}

	for i := 0; i < len(counts); i++ {
		for j := i + 1; j < len(counts); j++ {
			if counts[j].count > counts[i].count {
				counts[i], counts[j] = counts[j], counts[i]
			}
		}
	}

	if len(counts) > topN {
		counts = counts[:topN]
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)

	for _, nc := range counts {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(ns string, count int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if err := warmNameserverCache(ctx, ns, count); err != nil {
				log.Printf("Cache warm failed for %s: %v", ns, err)
			}
		}(nc.ns, nc.count)
	}

	wg.Wait()
}

func warmNameserverCache(ctx context.Context, ns string, totalCount int) error {
	query := "SELECT domain, rank FROM reverse_ns WHERE ns = ?"
	iter := db.Session.Query(query, ns).Iter()

	domains := make([]string, 0, totalCount)
	var domain string
	var rank int

	for iter.Scan(&domain, &rank) {
		domains = append(domains, domain)
	}

	if err := iter.Close(); err != nil {
		return err
	}

	zsetKey := fmt.Sprintf("ns:%s:domains", ns)
	pipe := cache.Client.Pipeline()

	for i, d := range domains {
		pipe.ZAdd(ctx, zsetKey, redis.Z{
			Score:  float64(i),
			Member: d,
		})
	}

	pipe.Set(ctx, fmt.Sprintf("ns:%s:count", ns), len(domains), 24*time.Hour)
	pipe.Expire(ctx, zsetKey, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	return err
}
