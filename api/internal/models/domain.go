package models

import "time"

// DomainMetadata represents the metadata for a single domain
type DomainMetadata struct {
	Domain    string    `json:"domain"`
	NSList    []string  `json:"ns_list"`
	IPList    []string  `json:"ip_list,omitempty"`
	Rank      int       `json:"rank"`
	CreatedAt time.Time `json:"created_at"`
}

// ReverseNSResponse represents the paginated API response
type ReverseNSResponse struct {
	Nameserver     string   `json:"nameserver"`
	Total          int64    `json:"total"`
	Page           int      `json:"page"`
	Limit          int      `json:"limit"`
	Domains        []string `json:"domains"`
	Cursor         string   `json:"cursor,omitempty"`
	Cached         bool     `json:"cached"`
	ResponseTimeMS int64    `json:"response_time_ms"`
}

// HostingProvider represents a hosting provider with domain count
type HostingProvider struct {
	ProviderName string `json:"provider"`
	DomainCount  int64  `json:"domain_count"`
}

// HostingProvidersResponse represents the API response for hosting providers
type HostingProvidersResponse struct {
	Providers      []HostingProvider `json:"providers"`
	Total          int               `json:"total,omitempty"`
	Page           int               `json:"page,omitempty"`
	Limit          int               `json:"limit,omitempty"`
	ResponseTimeMS int64             `json:"response_time_ms"`
	Cached         bool              `json:"cached"`
}

// NSCount represents a nameserver with its domain count for a provider
type NSCount struct {
	Nameserver string `json:"nameserver"`
	Count      int64  `json:"count"`
}

// ProviderNSBreakdownResponse represents the API response for provider nameserver breakdown
type ProviderNSBreakdownResponse struct {
	Provider       string    `json:"provider"`
	TotalNS        int64     `json:"total_nameservers"`
	TotalDomains   int64     `json:"total_domains"`
	NSCounts       []NSCount `json:"nameservers"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	Cached         bool      `json:"cached"`
}
