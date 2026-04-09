package compression

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	
	"github.com/klauspost/compress/zstd"
)

// DomainManager handles domain-to-ID compression
// Uses a simple hash-based approach for 700M+ domains
// Domain -> uint64 ID mapping
// At 700M domains, this is much more efficient than storing strings repeatedly
var (
	domainIDMap sync.Map
	idCounter   uint64
	idMutex     sync.Mutex
)

type GlobalCompression struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

var compression *GlobalCompression

func init() {
	e, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	d, _ := zstd.NewReader(nil)
	compression = &GlobalCompression{
		encoder: e,
		decoder: d,
	}
}

// GetDomainID returns or creates a unique ID for a domain
// Uses hash for deterministic IDs, works across multiple instances
func GetDomainID(domain string) uint64 {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return 0
	}

	// Try to get existing ID
	if id, ok := domainIDMap.Load(domain); ok {
		return id.(uint64)
	}

	// Generate new ID using hash (deterministic)
	h := fnv.New64a()
	h.Write([]byte(domain))
	id := h.Sum64()

	// Store and return
	domainIDMap.Store(domain, id)
	return id
}

// BatchGetDomainIDs processes multiple domains at once
func BatchGetDomainIDs(domains []string) []uint64 {
	ids := make([]uint64, len(domains))
	for i, domain := range domains {
		ids[i] = GetDomainID(domain)
	}
	return ids
}

// CompressDomainIDs compresses a list of domain IDs into a compact binary format
// Returns compressed bytes ready for storage
func CompressDomainIDs(ids []uint64) []byte {
	if len(ids) == 0 {
		return nil
	}

	// Convert to binary format (8 bytes per ID)
	data := make([]byte, len(ids)*8)
	for i, id := range ids {
		offset := i * 8
		binary.BigEndian.PutUint64(data[offset:offset+8], id)
	}

	// Compress with zstd
	return compression.encoder.EncodeAll(data, nil)
}

// DecompressDomainIDs decompresses compressed domain IDs
func DecompressDomainIDs(compressed []byte) ([]uint64, error) {
	if len(compressed) == 0 {
		return nil, nil
	}

	// Decompress
	data, err := compression.decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("decompression failed: %w", err)
	}

	// Convert from binary
	ids := make([]uint64, len(data)/8)
	for i := 0; i < len(data); i += 8 {
		ids[i/8] = binary.BigEndian.Uint64(data[i : i+8])
	}

	return ids, nil
}

// CompressDomainBytes compresses raw domain strings for storage
// Used when storing domain lists in compressed blob format
func CompressDomainBytes(domains []byte) []byte {
	if len(domains) == 0 {
		return nil
	}
	return compression.encoder.EncodeAll(domains, nil)
}

// DecompressDomainBytes decompresses compressed domain strings
func DecompressDomainBytes(compressed []byte) ([]byte, error) {
	if len(compressed) == 0 {
		return nil, nil
	}
	return compression.decoder.DecodeAll(compressed, nil)
}

// CompressAndConvertNSDomains compresses all domains for a nameserver
// Uses domain IDs + compression for maximum space efficiency
func CompressAndConvertNSDomains(domains []string) ([]byte, error) {
	// Get IDs
	ids := BatchGetDomainIDs(domains)
	
	// Compress the IDs
	compressed := CompressDomainIDs(ids)
	
	return compressed, nil
}

// GetDomainCache uses domain ID compression for Redis storage
// Instead of storing full domain strings, stores compressed ID list
func GetDomainCache(domains []string) []byte {
	if len(domains) == 0 {
		return nil
	}
	
	// Join domains with separator
	domainBytes := []byte(strings.Join(domains, "\n"))
	
	// Compress
	return compression.encoder.EncodeAll(domainBytes, nil)
}

// EstimateCompressionRatio returns estimated size reduction
func EstimateCompressionRatio(originalSize int, compressedSize int) float64 {
	if originalSize == 0 {
		return 0
	}
	return float64(originalSize) / float64(compressedSize)
}