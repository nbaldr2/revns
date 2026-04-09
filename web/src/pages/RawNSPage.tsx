import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, Copy, Database, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { getAllDomains } from '../api'

export default function RawNSPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [copied, setCopied] = useState(false)

  const nameserver = useMemo(() => (searchParams.get('ns') ?? '').trim().toLowerCase(), [searchParams])

  const allDomainsQuery = useQuery({
    queryKey: ['raw-all-domains', nameserver],
    queryFn: ({ signal }) => getAllDomains(nameserver, signal),
    enabled: Boolean(nameserver),
  })

  const rawText = allDomainsQuery.data?.domains.join('\n') ?? ''

  return (
    <div className="providers-page">
      <header className="providers-header">
        <button onClick={() => navigate('/providers')} className="back-button">
          <ArrowLeft size={20} />
          Back to Providers
        </button>
        <h1>Raw Domains</h1>
        <p className="subtitle">Nameserver: {nameserver || 'N/A'}</p>
      </header>

      {!nameserver && <p className="message error">Missing nameserver. Open this page with ?ns=&lt;nameserver&gt;.</p>}

      {allDomainsQuery.isLoading && (
        <div className="loading-state">
          <Loader2 size={36} className="spinner" />
          <p>Loading all domains...</p>
        </div>
      )}

      {allDomainsQuery.isError && (
        <p className="message error">{(allDomainsQuery.error as Error).message || 'Failed to load domains.'}</p>
      )}

      {allDomainsQuery.data && (
        <section className="panel">
          <div className="panel-header">
            <h2>
              <Database size={18} />
              {allDomainsQuery.data.total.toLocaleString()} domains
            </h2>
            <button
              type="button"
              className="copy-all-button"
              onClick={async () => {
                await navigator.clipboard.writeText(rawText)
                setCopied(true)
                setTimeout(() => setCopied(false), 1200)
              }}
              disabled={!rawText}
            >
              <Copy size={14} /> {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>

          <pre className="raw-json">{rawText || 'No domains found for this nameserver.'}</pre>
        </section>
      )}
    </div>
  )
}
