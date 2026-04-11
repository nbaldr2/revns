export interface ReverseNSResponse {
  nameserver: string
  total: number
  page: number
  limit: number
  domains: string[]
  cursor?: string
  cached: boolean
  response_time_ms: number
}

export interface AllDomainsResponse {
  nameserver: string
  total: number
  domains: string[]
  response_time_ms: number
}

export interface HostingProvider {
  provider: string
  domain_count: number
}

export interface HostingProvidersResponse {
  providers: HostingProvider[]
  total?: number
  page?: number
  limit?: number
  response_time_ms: number
  cached: boolean
}

export interface ProviderNSCount {
  nameserver: string
  count: number
}

export interface ProviderNSBreakdownResponse {
  provider: string
  total_nameservers: number
  total_domains: number
  nameservers: ProviderNSCount[]
  response_time_ms: number
  cached: boolean
}

// Domain to nameserver mapping for provider search
export interface DomainNSMapping {
  domain: string
  nameserver: string
}

export interface UploadStatus {
  filename: string
  status: 'uploading' | 'processing' | 'completed' | 'error'
  total_rows: number
  processed_rows: number
  inserted_rows: number
  errors: number
  failed_rows: number
  current_batch: number
  start_time: string
  elapsed_seconds: number
  message?: string
  last_error?: string
}

export interface FailedRow {
  line: number
  raw: string
  reason: string
}

export interface UploadErrorsResponse {
  filename: string
  failed_rows: FailedRow[]
  count: number
}

export interface GlobalStatsResponse {
  providers: number
  nameservers: number
  total_domains: number
  total_rows: number      // Raw rows uploaded (includes duplicates)
  response_time_ms: number
}

// Analytics types
export interface TimeSeriesData {
  date: string
  value: number
}

export interface TopProvider {
  name: string
  count: number
  percentage: number
}

export interface CurrentStats {
  TotalDomains: number
  TotalProviders: number
  TotalNameservers: number
  DataPoints: number
}

export interface Growth {
  DomainGrowth: number
  DomainGrowthPct: number
  ProviderGrowth: number
  ProviderGrowthPct: number
  NSGrowth: number
  NSGrowthPct: number
}

export interface TimeRange {
  Days: number
  StartDate: string
  EndDate: string
}

export interface AnalyticsSummary {
  CurrentStats: CurrentStats
  Growth: Growth
  TopProviders: TopProvider[]
  TimeRange: TimeRange
  total_uploads: number
  total_rows: number
  success_rate: number
  avg_rows_per_upload: number
}

export interface DailyStat {
  date: string
  total_domains: number
  new_domains: number
}

export interface TopNameserver {
  ns: string
  total_domains: number
}

export interface DashboardAnalytics {
  DailyStats: DailyStat[]
  TopNameservers: TopNameserver[]
  uploads_by_day: TimeSeriesData[]
  top_providers: TopProvider[]
  ns_distribution: { name: string; value: number }[]
  recent_uploads: UploadStatus[]
}
