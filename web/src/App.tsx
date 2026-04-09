import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, Building2, ChevronLeft, ChevronRight, Copy, Database, Globe, Search, Server, Code, Loader2, Upload } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { getAllDomains, getReverseNS, getTopHostingProviders, getGlobalStats } from './api'
import ProviderNSModal from './components/ProviderNSModal'
import type { HostingProvider } from './types'

const DEFAULT_NS = ''

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

function App() {
  const navigate = useNavigate()
  const [input, setInput] = useState(DEFAULT_NS)
  const [nameserver, setNameserver] = useState(DEFAULT_NS)
  const [page, setPage] = useState(1)
  const [limit, setLimit] = useState(100)
  const [copied, setCopied] = useState<string | null>(null)
  const [showRaw, setShowRaw] = useState(false)
  const [allCopied, setAllCopied] = useState(false)
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const ns = params.get('ns')
    const pageParam = Number(params.get('page'))
    const limitParam = Number(params.get('limit'))

    if (ns) {
      setNameserver(ns)
      setInput(ns)
    }
    if (Number.isFinite(pageParam) && pageParam > 0) setPage(pageParam)
    if (Number.isFinite(limitParam) && [25, 50, 100, 250, 500, 1000].includes(limitParam)) {
      setLimit(limitParam)
    }
  }, [])

  useEffect(() => {
    const params = new URLSearchParams()
    params.set('ns', nameserver)
    params.set('page', String(page))
    params.set('limit', String(limit))
    const next = `${window.location.pathname}?${params.toString()}`
    window.history.replaceState(null, '', next)
  }, [nameserver, page, limit])

  const reverseQuery = useQuery({
    queryKey: ['reverse-ns', nameserver, page, limit],
    queryFn: ({ signal }) => getReverseNS(nameserver, page, limit, signal),
    enabled: Boolean(nameserver.trim()),
    placeholderData: (previousData) => previousData,
  })

  const allDomainsQuery = useQuery({
    queryKey: ['all-domains', nameserver],
    queryFn: ({ signal }) => getAllDomains(nameserver, signal),
    enabled: false,
  })

  const topProvidersQuery = useQuery({
    queryKey: ['hosting-providers-top'],
    queryFn: ({ signal }) => getTopHostingProviders(signal),
    staleTime: 5 * 60 * 1000,
    refetchOnMount: true,
    refetchOnWindowFocus: true,
  })

  const globalStatsQuery = useQuery({
    queryKey: ['global-stats'],
    queryFn: ({ signal }) => getGlobalStats(signal),
    staleTime: 2 * 60 * 1000,
    refetchOnMount: true,
    refetchOnWindowFocus: true,
  })

  const totalPages = useMemo(() => {
    if (!reverseQuery.data?.total) return 1
    return Math.max(1, Math.ceil(reverseQuery.data.total / limit))
  }, [reverseQuery.data?.total, limit])

  const hasNextPage = page < totalPages
  const hasPreviousPage = page > 1

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmed = input.trim().toLowerCase()
    if (!trimmed) return
    setPage(1)
    setNameserver(trimmed)
  }

  const copyDomain = async (domain: string) => {
    await navigator.clipboard.writeText(domain)
    setCopied(domain)
    setTimeout(() => setCopied(null), 1200)
  }

  return (
    <main className="container">
      <section className="hero">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem', flexWrap: 'wrap' }}>
          <div>
            <h1>REVNS Lookup</h1>
           </div>
          <button type="button" className="secondary" onClick={() => navigate('/upload')}>
            <Upload size={16} />
            Upload CSV
          </button>
        </div>
      </section>

       
 
      <section className="stats-grid">
        {globalStatsQuery.data && (
          <>
            <article className="stat-card primary-stat">
              <div className="stat-title">
                <Upload size={24} /> Rows Uploaded
              </div>
              <strong className="stat-value">{formatNumber(globalStatsQuery.data.total_rows)}</strong>
            </article>
            <article className="stat-card primary-stat">
              <div className="stat-title">
                <Database size={24} /> Total Domains
              </div>
              <strong className="stat-value">{formatNumber(globalStatsQuery.data.total_domains)}</strong>
            </article>
            <article className="stat-card primary-stat">
              <div className="stat-title">
                <Server size={24} /> Providers
              </div>
              <strong className="stat-value">{formatNumber(globalStatsQuery.data.providers)}</strong>
            </article>
            <article className="stat-card primary-stat">
              <div className="stat-title">
                <Globe size={24} /> Nameservers
              </div>
              <strong className="stat-value">{formatNumber(globalStatsQuery.data.nameservers)}</strong>
            </article>
          </>
        )}
        {globalStatsQuery.isLoading && (
          <>
            <article className="stat-card primary-stat loading">
              <div className="stat-title">
                <Loader2 size={24} className="spinner" /> Rows Uploaded
              </div>
              <strong className="stat-value pulse">Loading...</strong>
            </article>
            <article className="stat-card primary-stat loading">
              <div className="stat-title">
                <Loader2 size={24} className="spinner" /> Total Domains
              </div>
              <strong className="stat-value pulse">Loading...</strong>
            </article>
            <article className="stat-card primary-stat loading">
              <div className="stat-title">
                <Loader2 size={24} className="spinner" /> Providers
              </div>
              <strong className="stat-value pulse">Loading...</strong>
            </article>
            <article className="stat-card primary-stat loading">
              <div className="stat-title">
                <Loader2 size={24} className="spinner" /> Nameservers
              </div>
              <strong className="stat-value pulse">Loading...</strong>
            </article>
          </>
        )}
      </section>

      <section className="panel providers-section">
        <div className="panel-header">
          <h2>
            <Building2 size={20} />
            Top 10 Hosting Providers
          </h2>
        </div>
        
        {topProvidersQuery.isLoading && (
          <div className="loading-skeleton">
            <div className="skeleton-row" />
            <div className="skeleton-row" />
            <div className="skeleton-row" />
            <div className="skeleton-row" />
            <div className="skeleton-row" />
          </div>
        )}
        
        {topProvidersQuery.isError && (
          <p className="message error">
            {(topProvidersQuery.error as Error).message || 'Failed to load providers'}
          </p>
        )}

        {topProvidersQuery.data && topProvidersQuery.data.providers.length > 0 && (
          <>
            <div className="providers-list">
              {topProvidersQuery.data.providers.map((provider: HostingProvider, index: number) => (
                <button
                  key={provider.provider}
                  className="provider-item"
                  onClick={() => {
                    setSelectedProvider(provider.provider)
                  }}
                >
                  <span className="provider-rank">#{index + 1}</span>
                  <div className="provider-details">
                    <span className="provider-name">{provider.provider}</span>
                    <span className="provider-count">
                      <Globe size={12} />
                      {formatNumber(provider.domain_count)} domains
                    </span>
                  </div>
                </button>
              ))}
            </div>
            <button className="see-more-button" onClick={() => navigate('/providers')}>
              See More
              <ChevronRight size={16} />
            </button>
          </>
        )}

        {topProvidersQuery.data && topProvidersQuery.data.providers.length === 0 && (
          <p className="message">No providers data available</p>
        )}
      </section>

      <section className="panel">
        <form className="search-form" onSubmit={handleSearch}>
          <label htmlFor="nameserver">Nameserver</label>
          <div className="search-row">
            <input
              id="nameserver"
              type="text"
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder="e.g. ns1.cloudflare.com"
            />
            <button type="submit" className="primary">
              <Search size={16} />
              Lookup
            </button>
          </div>

          <div className="controls-row">
            <label htmlFor="limit">Limit</label>
            <select
              id="limit"
              value={limit}
              onChange={(event) => {
                setPage(1)
                setLimit(Number(event.target.value))
              }}
            >
              {[25, 50, 100, 250, 500, 1000].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>

            <button
              type="button"
              className="secondary"
              onClick={() => allDomainsQuery.refetch()}
              disabled={allDomainsQuery.isFetching || !nameserver.trim()}
            >
              <Database size={16} />
              {allDomainsQuery.isFetching ? 'Loading all...' : 'Fetch all domains'}
            </button>

            <button
              type="button"
              className={showRaw ? 'secondary active' : 'secondary'}
              onClick={() => {
                if (!nameserver.trim()) {
                  return
                }
                if (!showRaw) {
                  allDomainsQuery.refetch()
                }
                setShowRaw((prev) => !prev)
              }}
              disabled={allDomainsQuery.isFetching || !nameserver.trim()}
            >
              {allDomainsQuery.isFetching && showRaw === false ? <Loader2 size={16} className="spinner" /> : <Code size={16} />}
              {allDomainsQuery.isFetching && showRaw === false ? 'Loading raw...' : 'Raw'}
            </button>
          </div>
        </form>
      </section>

      <section className="stats-grid">
        <article className="stat-card">
          <div className="stat-title">
            <Server size={16} /> Nameserver
          </div>
          <strong>{reverseQuery.data?.nameserver ?? nameserver}</strong>
        </article>
        <article className="stat-card">
          <div className="stat-title">
            <Database size={16} /> Total Domains
          </div>
          <strong>{formatNumber(reverseQuery.data?.total ?? 0)}</strong>
        </article>
        <article className="stat-card">
          <div className="stat-title">
            <Activity size={16} /> Response Time
          </div>
          <strong>{reverseQuery.data?.response_time_ms ?? 0} ms</strong>
        </article>
        <article className="stat-card">
          <div className="stat-title">
            <Database size={16} /> Cache
          </div>
          <strong>{reverseQuery.data?.cached ? 'Hit / Shared' : 'Miss'}</strong>
        </article>
      </section>
 

      <section className="panel">
        <div className="panel-header">
          <h2>Domains</h2>
          {!showRaw && (
            <div className="pagination">
              <button onClick={() => setPage((prev) => prev - 1)} disabled={!hasPreviousPage}>
                <ChevronLeft size={16} /> Prev
              </button>
              <span>
                Page {page} / {totalPages}
              </span>
              <button onClick={() => setPage((prev) => prev + 1)} disabled={!hasNextPage}>
                Next <ChevronRight size={16} />
              </button>
            </div>
          )}
          {showRaw && allDomainsQuery.data && (
            <button
              type="button"
              className="copy-all-button"
              onClick={async () => {
                await navigator.clipboard.writeText(allDomainsQuery.data.domains.join('\n'))
                setAllCopied(true)
                setTimeout(() => setAllCopied(false), 1200)
              }}
            >
              <Copy size={14} /> {allCopied ? 'Copied!' : 'Copy'}
            </button>
          )}
        </div>

        {reverseQuery.isLoading && (
          <div className="loading-state">
            <Loader2 size={32} className="spinner" />
            <p>Loading domains...</p>
          </div>
        )}
        {reverseQuery.isError && (
          <p className="message error">{(reverseQuery.error as Error).message || 'Something went wrong.'}</p>
        )}

        {reverseQuery.data && reverseQuery.data.domains.length === 0 && !showRaw && (
          <p className="message">No domains found for this nameserver.</p>
        )}

        {reverseQuery.data && reverseQuery.data.domains.length > 0 && !showRaw && (
          <ul className="domain-list">
            {reverseQuery.data.domains.map((domain) => (
              <li key={domain}>
                <span>{domain}</span>
                <button type="button" onClick={() => copyDomain(domain)}>
                  <Copy size={14} /> {copied === domain ? 'Copied' : 'Copy'}
                </button>
              </li>
            ))}
          </ul>
        )}

        {showRaw && allDomainsQuery.data && (
          <pre className="raw-json">{allDomainsQuery.data.domains.join('\n')}</pre>
        )}

        {showRaw && !allDomainsQuery.data && !allDomainsQuery.isFetching && (
          <p className="message">No raw data loaded yet. Click &quot;Raw&quot; again to fetch.</p>
        )}
      </section>

      {allDomainsQuery.data && !showRaw && (
        <section className="panel">
          <h2>All Domains Snapshot</h2>
          <p className="message">
            Retrieved {formatNumber(allDomainsQuery.data.total)} total domains in{' '}
            {allDomainsQuery.data.response_time_ms} ms.
          </p>
        </section>
      )}

      <ProviderNSModal
        provider={selectedProvider}
        open={Boolean(selectedProvider)}
        onClose={() => setSelectedProvider(null)}
      />
    </main>
  )
}

export default App
