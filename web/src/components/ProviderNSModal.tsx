import { useQuery } from '@tanstack/react-query'
import { AlertCircle, Loader2, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { getProviderNSBreakdown } from '../api'

interface ProviderNSModalProps {
  provider: string | null
  open: boolean
  onClose: () => void
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

export default function ProviderNSModal({ provider, open, onClose }: ProviderNSModalProps) {
  const navigate = useNavigate()

  const breakdownQuery = useQuery({
    queryKey: ['provider-ns-breakdown', provider],
    queryFn: () => getProviderNSBreakdown(provider as string),
    enabled: open && Boolean(provider),
    staleTime: 5 * 60 * 1000,
  })

  if (!open || !provider) return null

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section className="modal-panel" onClick={(event) => event.stopPropagation()}>
        <header className="modal-header">
          <div>
            <h3>{provider} nameservers</h3>
            {breakdownQuery.data && (
              <p>
                {formatNumber(breakdownQuery.data.total_nameservers)} unique NS •{' '}
                {formatNumber(breakdownQuery.data.total_domains)} rows
              </p>
            )}
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Close modal">
            <X size={16} />
          </button>
        </header>

        {breakdownQuery.isLoading && (
          <div className="modal-state">
            <Loader2 size={18} className="spinner" /> Loading nameservers...
          </div>
        )}

        {breakdownQuery.isError && (
          <div className="modal-state error">
            <AlertCircle size={18} /> {(breakdownQuery.error as Error).message || 'Failed to load breakdown.'}
          </div>
        )}

        {breakdownQuery.data && breakdownQuery.data.nameservers.length === 0 && (
          <div className="modal-state">
            <AlertCircle size={18} /> No nameservers found for this provider. The provider_ns table may not be populated.
          </div>
        )}

        {breakdownQuery.data && (
          <ul className="ns-breakdown-list">
            {breakdownQuery.data.nameservers.map((item) => (
              <li key={item.nameserver}>
                <span>{item.nameserver}</span>
                <div className="ns-row-actions">
                  <strong>{formatNumber(item.count)}</strong>
                  <button
                    type="button"
                    className="ns-fetch-button"
                    onClick={() => {
                      onClose()
                      navigate(`/raw?ns=${encodeURIComponent(item.nameserver)}`)
                    }}
                  >
                    Fetch
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
