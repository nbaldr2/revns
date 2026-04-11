import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ArrowLeft, Building2, Globe, Loader2, AlertCircle, Search, X, Zap } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getHostingProvidersPage, searchProviderDomain } from '../api'
import ProviderNSModal from '../components/ProviderNSModal'
import type { DomainNSMapping } from '../types'

const PAGE_SIZE = 100

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

export default function ProvidersPage() {
  const navigate = useNavigate()
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null)
  const [loadingAll, setLoadingAll] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [fastSearchNS, setFastSearchNS] = useState('')
  const [fastSearchEnabled, setFastSearchEnabled] = useState(false)

  const fastSearchQuery = useQuery({
    queryKey: ['fast-provider-search', fastSearchNS],
    queryFn: ({ signal }) => searchProviderDomain(fastSearchNS, signal),
    enabled: fastSearchEnabled && fastSearchNS.trim().length > 0,
    staleTime: 5 * 60 * 1000,
  })

  const providersQuery = useInfiniteQuery({
    queryKey: ['hosting-providers-paged', PAGE_SIZE, 'v2'],
    queryFn: ({ pageParam, signal }) => getHostingProvidersPage(pageParam as number, PAGE_SIZE, signal),
    initialPageParam: 1,
    getNextPageParam: (lastPage) => {
      const page = lastPage.page ?? 1
      const limit = lastPage.limit ?? PAGE_SIZE
      const total = lastPage.total ?? 0
      if (page * limit >= total) return undefined
      return page + 1
    },
    staleTime: 0,
    gcTime: 5 * 60 * 1000,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
  })

  const providers = useMemo(() => {
    if (!providersQuery.data) return []
    return providersQuery.data.pages.flatMap((p) => p.providers)
  }, [providersQuery.data])

  const normalizedSearch = searchTerm.trim().toLowerCase()

  const filteredProviders = useMemo(() => {
    if (!normalizedSearch) return providers
    return providers.filter((provider) => provider.provider.toLowerCase().includes(normalizedSearch))
  }, [providers, normalizedSearch])

  const totalProviders = providersQuery.data?.pages[0]?.total ?? providers.length
  const totalDomainsLoaded = useMemo(
    () => providers.reduce((sum, p) => sum + p.domain_count, 0),
    [providers],
  )

  const handleLoadAll = async () => {
    if (!providersQuery.hasNextPage) return
    setLoadingAll(true)
    try {
      while (providersQuery.hasNextPage) {
        // eslint-disable-next-line no-await-in-loop
        await providersQuery.fetchNextPage()
      }
    } finally {
      setLoadingAll(false)
    }
  }

  // Generate the <pre> formatted results
  const generateResultsPre = (domains: DomainNSMapping[]): string => {
    if (domains.length === 0) return ''
    const lines = domains.map((d) => `${d.domain} | ${d.nameserver}`)
    return lines.join('\n')
  }

  return (
    <div className="providers-page">
      <header className="providers-header">
        <button onClick={() => navigate('/')} className="back-button">
          <ArrowLeft size={20} />
          Back to Lookup
        </button>
        <h1>All Hosting Providers</h1>
        <p className="subtitle">Fast loading with lazy pagination (100 per batch)</p>

        <div className="provider-search-wrap">
          <div className="provider-search-input-wrap">
            <Search size={16} />
            <input
              type="text"
              value={searchTerm}
              onChange={(event) => setSearchTerm(event.target.value)}
              placeholder="Search provider name..."
              aria-label="Search provider name"
            />
            {searchTerm && (
              <button
                type="button"
                className="provider-search-clear"
                onClick={() => setSearchTerm('')}
                aria-label="Clear provider search"
              >
                <X size={14} />
              </button>
            )}
          </div>
          <p className="provider-search-hint">
            Showing {formatNumber(filteredProviders.length)} of {formatNumber(providers.length)} loaded providers
          </p>
        </div>

        {/* Fast Provider Search */}
        <div className="fast-search-section">
          <div className="fast-search-header">
            <Zap size={20} />
            <h2>Fast Provider Search</h2>
          </div>
          <p className="fast-search-description">
            Enter a provider domain (e.g., cloudflare.com) to find all domains using ANY nameserver ending with that provider domain
          </p>
          <div className="fast-search-input-group">
            <input
              type="text"
              value={fastSearchNS}
              onChange={(e) => setFastSearchNS(e.target.value)}
              placeholder="Enter provider domain (e.g., cloudflare.com)"
              className="fast-search-input"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && fastSearchNS.trim()) {
                  setFastSearchEnabled(true)
                  fastSearchQuery.refetch()
                }
              }}
            />
            <button
              type="button"
              className="fast-search-button"
              onClick={() => {
                if (fastSearchNS.trim()) {
                  setFastSearchEnabled(true)
                  fastSearchQuery.refetch()
                }
              }}
              disabled={!fastSearchNS.trim() || fastSearchQuery.isFetching}
            >
              {fastSearchQuery.isFetching ? (
                <Loader2 size={16} className="spinner" />
              ) : (
                <Search size={16} />
              )}
              Search
            </button>
            {fastSearchEnabled && (
              <button
                type="button"
                className="fast-search-clear"
                onClick={() => {
                  setFastSearchNS('')
                  setFastSearchEnabled(false)
                }}
              >
                <X size={16} />
                Clear
              </button>
            )}
            {fastSearchEnabled && fastSearchNS.trim() && (
              <a
                className="fast-search-download"
                href={`/api/v1/provider-search.csv?domain=${encodeURIComponent(fastSearchNS.trim())}`}
              >
                Download CSV
              </a>
            )}
          </div>

          {/* Fast Search Results */}
          {fastSearchEnabled && fastSearchQuery.data && (
            <div className="fast-search-results">
              <div className="fast-search-results-header">
                <Globe size={18} />
                <span>
                  Found <strong>{formatNumber(fastSearchQuery.data.total)}</strong> domains using{' '}
                  <strong>{fastSearchQuery.data.provider_domain}</strong> nameservers
                </span>
                <span className="fast-search-time">
                  ({fastSearchQuery.data.response_time_ms}ms)
                </span>
              </div>
              
              {fastSearchQuery.data.domains.length > 0 ? (
                <div className="fast-search-pre-container">
                  <pre className="fast-search-pre">
{`Found ${fastSearchQuery.data.total} domains using ${fastSearchQuery.data.provider_domain} nameservers.
Showing ${formatNumber(fastSearchQuery.data.returned)} results (max 10,000). Download CSV for full list.

${generateResultsPre(fastSearchQuery.data.domains)}`}
                  </pre>
                </div>
              ) : (
                <p className="fast-search-empty">No domains found for this provider.</p>
              )}
            </div>
          )}

          {fastSearchEnabled && fastSearchQuery.isError && (
            <div className="fast-search-error">
              <AlertCircle size={18} />
              <span>Error: {(fastSearchQuery.error as Error).message}</span>
            </div>
          )}
        </div>
      </header>

      {providersQuery.isLoading && (
        <div className="loading-state">
          <Loader2 size={48} className="spinner" />
          <p>Loading providers...</p>
        </div>
      )}

      {providersQuery.isError && (
        <div className="error-state">
          <AlertCircle size={48} />
          <p>Error loading providers</p>
          <p className="error-message">
            {(providersQuery.error as Error).message || 'Something went wrong'}
          </p>
        </div>
      )}

      {providers.length > 0 && (
        <>
          <div className="stats-summary">
            <div className="summary-card">
              <Building2 size={24} />
              <div>
                <span className="summary-label">Loaded Providers</span>
                <span className="summary-value">
                  {formatNumber(filteredProviders.length)} / {formatNumber(totalProviders)}
                </span>
              </div>
            </div>
            <div className="summary-card">
              <Globe size={24} />
              <div>
                <span className="summary-label">Domains (Loaded Rows)</span>
                <span className="summary-value">{formatNumber(totalDomainsLoaded)}</span>
              </div>
            </div>
          </div>

          {filteredProviders.length > 0 ? (
            <div className="providers-grid">
              {filteredProviders.map((provider, index) => (
                <button
                  type="button"
                  key={`${provider.provider}-${index}`}
                  className="provider-card"
                  onClick={() => setSelectedProvider(provider.provider)}
                >
                  <div className="provider-rank">#{index + 1}</div>
                  <div className="provider-info">
                    <h3 className="provider-name">{provider.provider}</h3>
                    <div className="provider-stats">
                      <Globe size={16} />
                      <span>{formatNumber(provider.domain_count)} domains</span>
                    </div>
                  </div>
                  <div className="provider-percentage">
                    {(totalDomainsLoaded > 0 ? (provider.domain_count / totalDomainsLoaded) * 100 : 0).toFixed(1)}%
                  </div>
                </button>
              ))}
            </div>
          ) : (
            <p className="message">No provider matches your search.</p>
          )}

          <div className="load-more-wrap">
            {providersQuery.hasNextPage ? (
              <div className="load-more-actions">
                <button
                  type="button"
                  className="see-more-button"
                  onClick={() => providersQuery.fetchNextPage()}
                  disabled={providersQuery.isFetchingNextPage || loadingAll}
                >
                  {providersQuery.isFetchingNextPage ? (
                    <>
                      <Loader2 size={16} className="spinner" /> Loading more
                    </>
                  ) : (
                    'Load More'
                  )}
                </button>
                <button
                  type="button"
                  className="secondary"
                  onClick={handleLoadAll}
                  disabled={loadingAll || providersQuery.isFetchingNextPage}
                >
                  {loadingAll ? (
                    <>
                      <Loader2 size={16} className="spinner" /> Loading all
                    </>
                  ) : (
                    'Load All'
                  )}
                </button>
              </div>
            ) : (
              <p className="response-time">All providers loaded.</p>
            )}
          </div>

          {providersQuery.data?.pages[0]?.response_time_ms ? (
            <p className="response-time">
              First page response time: {providersQuery.data.pages[0].response_time_ms}ms
              {providersQuery.data.pages[0].cached ? ' (cached)' : ''}
            </p>
          ) : null}

          <ProviderNSModal
            provider={selectedProvider}
            open={Boolean(selectedProvider)}
            onClose={() => setSelectedProvider(null)}
          />
        </>
      )}
    </div>
  )
}
