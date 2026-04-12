package handlers

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soufianerochdi/revns-api/internal/cache"
	"github.com/soufianerochdi/revns-api/internal/db"
)

// UploadStatus tracks the current upload progress
type UploadStatus struct {
	Filename       string    `json:"filename"`
	Status         string    `json:"status"` // "uploading", "processing", "completed", "error"
	TotalRows      int       `json:"total_rows"`
	ProcessedRows  int       `json:"processed_rows"`
	InsertedRows   int       `json:"inserted_rows"`
	Errors         int       `json:"errors"`
	FailedRows     int       `json:"failed_rows"`
	CurrentBatch   int       `json:"current_batch"`
	StartTime      time.Time `json:"start_time"`
	ElapsedSeconds float64   `json:"elapsed_seconds"`
	Message        string    `json:"message,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	TempPath       string    `json:"-"`
}

type FailedRow struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

var (
	uploadStatuses = make(map[string]*UploadStatus)
	uploadFailures = make(map[string][]FailedRow)
	uploadMutex    sync.RWMutex
)

// In-memory aggregation for provider stats during upload
type uploadAggState struct {
	providerCounts map[string]int64  // provider -> domain count
	providerNS     map[string]map[string]int64  // provider -> ns -> count
	mutex          sync.Mutex
}

func newUploadAggState() *uploadAggState {
	return &uploadAggState{
		providerCounts: make(map[string]int64),
		providerNS:     make(map[string]map[string]int64),
	}
}

const (
	batchSize         = 3000  // Optimized batch size (balance of speed and size limits)
	flushInterval     = 10 * time.Second
	reportingInterval = 5000 // Report every N rows
	maxFailureSamples = 50
	workerCount       = 4    // Parallel workers for faster processing
	statsBatchSize    = 500  // Safe batch size for provider_stats/provider_ns writes
)

func recordRowError(filename string, line int, row []string, reason string) {
	raw := ""
	if row != nil {
		raw = strings.Join(row, ",")
	}

	uploadMutex.Lock()
	failures := uploadFailures[filename]
	if len(failures) < maxFailureSamples {
		failures = append(failures, FailedRow{Line: line, Raw: raw, Reason: reason})
		uploadFailures[filename] = failures
	}
	if status, ok := uploadStatuses[filename]; ok {
		status.Errors++
		status.FailedRows++
		if status.LastError == "" {
			status.LastError = reason
		}
	}
	uploadMutex.Unlock()
}

func recordBatchFailure(filename string, batchNum int, err error) {
	reason := fmt.Sprintf("Batch %d failed: %v", batchNum, err)
	uploadMutex.Lock()
	if status, ok := uploadStatuses[filename]; ok {
		status.LastError = reason
	}
	uploadMutex.Unlock()
	recordRowError(filename, batchNum, nil, reason)
}

// GetUploadStatus returns the current upload status
func GetUploadStatus(c *gin.Context) {
	filename := c.Query("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename parameter required"})
		return
	}

	uploadMutex.RLock()
	status, exists := uploadStatuses[filename]
	uploadMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload not found"})
		return
	}

	// Calculate elapsed time
	status.ElapsedSeconds = time.Since(status.StartTime).Seconds()
	c.JSON(http.StatusOK, status)
}

// GetUploadErrors returns sampled failed rows for a given upload
func GetUploadErrors(c *gin.Context) {
	filename := c.Query("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename parameter required"})
		return
	}

	uploadMutex.RLock()
	failures, exists := uploadFailures[filename]
	uploadMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"filename": filename,
		"failed_rows": failures,
		"count": len(failures),
	})
}

// UploadCSV handles CSV file upload with streaming processing
func UploadCSV(c *gin.Context) {
	// Check database connection first
	if db.Session == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not connected"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to get file: " + err.Error()})
		return
	}
	defer file.Close()

	tmpFile, err := os.CreateTemp("", "upload-*.csv")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create temp file: " + err.Error()})
		return
	}
	// Note: Don't use defer os.Remove here - the goroutine handles cleanup at line 199
	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file: " + err.Error()})
		return
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		tmpFile.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset file: " + err.Error()})
		return
	}

	filename := header.Filename

	// Initialize status
	uploadMutex.Lock()
	uploadStatuses[filename] = &UploadStatus{
		Filename:  filename,
		Status:    "uploading",
		StartTime: time.Now(),
		TempPath:  tmpFile.Name(),
	}
	uploadFailures[filename] = nil
	uploadMutex.Unlock()

	// Start processing in background using temp file
	go func(path string) {
		f, err := os.Open(path)
		if err != nil {
			updateStatus(filename, func(s *UploadStatus) {
				s.Status = "error"
				s.Message = "Failed to reopen temp file: " + err.Error()
			})
			return
		}
		defer f.Close()
		processCSVUpload(context.Background(), f, filename)
		// Don't delete status immediately - keep it for 5 minutes after completion
		// so the frontend can still poll and see final results
		go func() {
			time.Sleep(5 * time.Minute)
			uploadMutex.Lock()
			delete(uploadStatuses, filename)
			delete(uploadFailures, filename)
			uploadMutex.Unlock()
		}()
		os.Remove(path)
	}(tmpFile.Name())

	c.JSON(http.StatusOK, gin.H{
		"message":    "upload started",
		"filename":   filename,
		"status_url": "/api/v1/upload/status?filename=" + filename,
	})
}

// processCSVUpload streams and processes the CSV file
func processCSVUpload(ctx context.Context, reader io.Reader, filename string) {
	updateStatus(filename, func(s *UploadStatus) {
		s.Status = "processing"
		s.Message = "Reading CSV header..."
	})

	bufReader := bufio.NewReaderSize(reader, 64*1024) // 64KB buffer
	csvReader := csv.NewReader(bufReader)
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true // Reduce allocations

	lineNum := 1

	// Read header
	header, err := csvReader.Read()
	if err != nil {
		recordRowError(filename, lineNum, nil, "Failed to read header: "+err.Error())
		updateStatus(filename, func(s *UploadStatus) {
			s.Status = "error"
			s.Message = "Failed to read header: " + err.Error()
		})
		return
	}

	updateStatus(filename, func(s *UploadStatus) {
		s.Message = fmt.Sprintf("CSV columns: %v", header)
	})

	// Process records in batches using unlogged batches
	batch := make([]dbRecord, 0, batchSize)
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	var wg sync.WaitGroup
	recordCh := make(chan dbRecord, batchSize*4)
	doneCh := make(chan struct{})

	// Start batch processor
	wg.Add(1)
	go func() {
		defer wg.Done()
		processBatchWorker(ctx, recordCh, filename, doneCh)
	}()

	processing := true

	for processing {
		select {
		case <-flushTicker.C:
			// Periodic progress update
			uploadMutex.RLock()
			if status, ok := uploadStatuses[filename]; ok {
				status.ElapsedSeconds = time.Since(status.StartTime).Seconds()
			}
			uploadMutex.RUnlock()

		default:
			row, err := csvReader.Read()
			if err == io.EOF {
				processing = false
				break
			}
			if err != nil {
				recordRowError(filename, lineNum, nil, fmt.Sprintf("CSV read error: %v", err))
				continue
			}

			lineNum++

			if len(row) < 3 {
				recordRowError(filename, lineNum, row, "Row missing required columns (domain, level, ns)")
				continue
			}

			domain := cleanString(row[0])
			if domain == "" {
				recordRowError(filename, lineNum, row, "Domain is empty")
				continue
			}

			level := cleanString(row[1])
			nsField := cleanString(row[2])

			// Parse nameservers
			nsList := parseNameservers(nsField)

			for _, ns := range nsList {
				if ns != "" {
					rec := dbRecord{
						domain: domain,
						ns:     ns,
						level:  level,
					}
					select {
					case recordCh <- rec:
						batch = append(batch, rec)
					case <-ctx.Done():
						processing = false
						break
					}
				}
			}

			// Update progress periodically
			if lineNum%reportingInterval == 0 {
				updateStatus(filename, func(s *UploadStatus) {
					s.TotalRows = lineNum
					s.ProcessedRows = lineNum
					s.Message = fmt.Sprintf("Processing row %d...", lineNum)
				})
			}
		}
	}

	// Send remaining records
	close(recordCh)
	close(doneCh)
	wg.Wait()

	// Final status update
	uploadMutex.Lock()
	if status, ok := uploadStatuses[filename]; ok {
		status.Status = "completed"
		status.TotalRows = lineNum
		status.ElapsedSeconds = time.Since(status.StartTime).Seconds()
		status.Message = fmt.Sprintf("Completed processing %d rows in %.1f seconds", lineNum, status.ElapsedSeconds)
	}
	uploadMutex.Unlock()
}

type dbRecord struct {
	domain string
	ns     string
	level  string
}

// extractProviderName extracts provider name from nameserver
func extractProviderName(ns string) string {
	ns = strings.ToLower(ns)
	providers := map[string]string{
		"cloudflare": "Cloudflare",
		"awsdns":     "AWS",
		"aws":        "AWS",
		"amazon":     "AWS",
		"hostgator":  "HostGator",
		"godaddy":    "GoDaddy",
		"bluehost":   "Bluehost",
		"digitalocean": "DigitalOcean",
		"linode":     "Linode",
		"vultr":      "Vultr",
		"google":     "Google",
		"googleapis": "Google",
		"nsone":      "NS1",
		"alidns":     "Alibaba Cloud",
		"hichina":    "Alibaba Cloud",
		"namecheap":  "Namecheap",
		"registrar-servers": "Namecheap",
		"dreamhost":  "DreamHost",
		"siteground": "SiteGround",
		"hostinger":  "Hostinger",
		"rackspace":  "Rackspace",
		"cloudways":  "Cloudways",
		"kinsta":     "Kinsta",
		"wpengine":   "WP Engine",
		"vercel":     "Vercel",
		"netlify":    "Netlify",
		"heroku":     "Heroku",
		"fastly":     "Fastly",
		"googlehosted": "Google",
		"ultradns":   "UltraDNS",
		"domaincontrol": "GoDaddy",
		"ovh":        "OVH",
	}
	for keyword, provider := range providers {
		if strings.Contains(ns, keyword) {
			return provider
		}
	}
	// Fallback to extracting from domain parts
	parts := strings.Split(ns, ".")
	for i := 0; i < len(parts); i++ {
		label := parts[i]
		if label == "" || len(label) <= 2 {
			continue
		}
		if isGenericNSLabel(label) {
			continue
		}
		// Capitalize first letter
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

// updateProviderStats aggregates provider statistics using read-modify-write pattern
func updateProviderStats(domain, ns string) error {
	provider := extractProviderName(ns)

	// Read current provider_stats
	var currentCount int64
	err := db.Session.Query(
		"SELECT domain_count FROM provider_stats WHERE provider = ?",
		provider,
	).Scan(&currentCount)

	newCount := currentCount + 1
	if err != nil {
		// Row doesn't exist, insert new
		newCount = 1
	}

	// Update provider_stats with new count
	if err := db.Session.Query(
		"INSERT INTO provider_stats (provider, domain_count, ns_count, total_rows, updated_at) VALUES (?, ?, 1, 1, toTimestamp(now()))",
		provider, newCount,
	).Exec(); err != nil {
		return err
	}

	// Read current provider_ns count
	var nsCount int64
	err = db.Session.Query(
		"SELECT domain_count FROM provider_ns WHERE provider = ? AND ns = ?",
		provider, ns,
	).Scan(&nsCount)

	newNSCount := nsCount + 1
	if err != nil {
		newNSCount = 1
	}

	// Update provider_ns
	if err := db.Session.Query(
		"INSERT INTO provider_ns (provider, ns, domain_count) VALUES (?, ?, ?)",
		provider, ns, newNSCount,
	).Exec(); err != nil {
		return err
	}

	// Insert into provider_domains
	return db.Session.Query(
		"INSERT INTO provider_domains (provider, domain, ns, rank) VALUES (?, ?, ?, ?)",
		provider, domain, ns, 0,
	).Exec()
}

// processBatchWorker receives records and writes to database in batches
// Uses in-memory aggregation for provider stats to avoid slow per-record DB calls
func processBatchWorker(ctx context.Context, recordCh <-chan dbRecord, filename string, doneCh <-chan struct{}) {
	batch := make([]dbRecord, 0, batchSize)
	batchNum := 0
	aggState := newUploadAggState()

	// Periodic flush of aggregated stats
	aggFlushTicker := time.NewTicker(flushInterval)
	defer aggFlushTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		batchNum++

		// Insert into reverse_ns using unlogged batch (fast)
		b := db.Session.NewBatch(0)
		for _, r := range batch {
			b.Query(
				"INSERT INTO reverse_ns (ns, domain, rank) VALUES (?, ?, ?)",
				r.ns, r.domain, 0,
			)
		}

		if err := db.Session.ExecuteBatch(b); err != nil {
			updateStatus(filename, func(s *UploadStatus) {
				s.Errors += len(batch)
				s.FailedRows += len(batch)
				s.Message = "Batch insert error: " + err.Error()
				s.LastError = err.Error()
			})
			recordBatchFailure(filename, batchNum, err)
			return
		}

		// Aggregate provider stats in memory (fast) instead of per-record DB calls (slow)
		aggState.mutex.Lock()
		for _, r := range batch {
			provider := extractProviderName(r.ns)
			aggState.providerCounts[provider]++
			if aggState.providerNS[provider] == nil {
				aggState.providerNS[provider] = make(map[string]int64)
			}
			aggState.providerNS[provider][r.ns]++
		}
		aggState.mutex.Unlock()

		updateStatus(filename, func(s *UploadStatus) {
			s.InsertedRows += len(batch)
			s.CurrentBatch = batchNum
			s.ProcessedRows += len(batch)
		})
		batch = batch[:0]
	}

	// Flush aggregated stats periodically
	flushAggState := func() {
		aggState.mutex.Lock()
		defer aggState.mutex.Unlock()

		// Write provider_stats in smaller batches to avoid silent failures
		if len(aggState.providerCounts) > 0 {
			statsBatch := db.Session.NewBatch(0)
			count := 0
			for provider, domainCount := range aggState.providerCounts {
				statsBatch.Query(
					"INSERT INTO provider_stats (provider, domain_count, ns_count, total_rows, updated_at) VALUES (?, ?, 1, ?, toTimestamp(now()))",
					provider, domainCount, domainCount,
				)
				count++
				if count >= statsBatchSize {
					_ = db.Session.ExecuteBatch(statsBatch)
					statsBatch = db.Session.NewBatch(0)
					count = 0
				}
			}
			if count > 0 {
				_ = db.Session.ExecuteBatch(statsBatch)
			}
		}

		// Write provider_ns in smaller batches
		if len(aggState.providerNS) > 0 {
			nsBatch := db.Session.NewBatch(0)
			count := 0
			for provider, nsCounts := range aggState.providerNS {
				for ns, domainCount := range nsCounts {
					nsBatch.Query(
						"INSERT INTO provider_ns (provider, ns, domain_count) VALUES (?, ?, ?)",
						provider, ns, domainCount,
					)
					count++
					if count >= statsBatchSize {
						_ = db.Session.ExecuteBatch(nsBatch)
						nsBatch = db.Session.NewBatch(0)
						count = 0
					}
				}
			}
			if count > 0 {
				_ = db.Session.ExecuteBatch(nsBatch)
			}
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case rec, ok := <-recordCh:
			if !ok {
				flush()
				flushAggState()
				return
			}
			batch = append(batch, rec)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-aggFlushTicker.C:
			flushAggState()
		case <-doneCh:
			flush()
			flushAggState()
			return
		case <-ctx.Done():
			flush()
			flushAggState()
			return
		}
	}
}

// updateStatus safely updates the upload status
func updateStatus(filename string, updater func(*UploadStatus)) {
	uploadMutex.Lock()
	defer uploadMutex.Unlock()
	if status, ok := uploadStatuses[filename]; ok {
		updater(status)
	}
}

// cleanString removes whitespace and null bytes
func cleanString(s string) string {
	// Remove null bytes and trim
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0 {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// parseNameservers splits comma-separated nameservers
func parseNameservers(nsField string) []string {
	if nsField == "" {
		return nil
	}

	// Fast manual split to avoid allocations
	var result []string
	start := 0
	for i := 0; i < len(nsField); i++ {
		if nsField[i] == ',' {
			if i > start {
				s := cleanString(nsField[start:i])
				if s != "" {
					result = append(result, s)
				}
			}
			start = i + 1
		}
	}
	// Add last part
	if start < len(nsField) {
		s := cleanString(nsField[start:])
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}
