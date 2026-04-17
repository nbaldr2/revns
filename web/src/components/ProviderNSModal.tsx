import { useQuery } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { AlertCircle, Download, Loader2, Search, X } from 'lucide-react'
import { useMemo, useRef, useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { getProviderNSBreakdown } from '../api'

interface ProviderNSModalProps {
  provider: string | null
  open: boolean
  onClose: () => void
}

interface NSItem {
  nameserver: string
  count: number
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

// Debounce hook for search
function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value)

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])

  return debouncedValue
}

// Skeleton row component
function SkeletonRow() {
  return (
    <div className="skeleton-row">
      <div className="skeleton-text" />
      <div className="skeleton-count" />
    </div>
  )
}

// Export to CSV
function exportToCSV(data: NSItem[], provider: string) {
  const headers = ['Nameserver', 'Domain Count']
  const rows = data.map(item => [item.nameserver, item.count])
  const csvContent = [headers, ...rows]
    .map(row => row.join(','))
    .join('\n')

  const blob = new Blob([csvContent], { type: 'text/csv' })
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${provider.toLowerCase().replace(/\s+/g, '_')}_nameservers.csv`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  window.URL.revokeObjectURL(url)
}

export default function ProviderNSModal({ provider, open, onClose }: ProviderNSModalProps) {
  const navigate = useNavigate()
  const [searchQuery, setSearchQuery] = useState('')
  const debouncedSearch = useDebounce(searchQuery, 300)
  const parentRef = useRef<HTMLDivElement>(null)

  // Smart caching with stale-while-revalidate pattern
  const breakdownQuery = useQuery({
    queryKey: ['provider-ns-breakdown', provider],
    queryFn: () => getProviderNSBreakdown(provider as string),
    enabled: open && Boolean(provider),
    staleTime: 30 * 60 * 1000, // 30 minutes - data doesn't change often
    gcTime: 60 * 60 * 1000, // Keep in cache for 1 hour
    refetchOnWindowFocus: false,
    placeholderData: (previousData) => previousData, // Show stale data while fetching
  })

  // Filter data locally based on search
  const filteredData = useMemo(() => {
    if (!breakdownQuery.data?.nameservers) return []
    if (!debouncedSearch) return breakdownQuery.data.nameservers

    const search = debouncedSearch.toLowerCase()
    return breakdownQuery.data.nameservers.filter(
      (item) =>
        item.nameserver.toLowerCase().includes(search) ||
        item.count.toString().includes(search)
    )
  }, [breakdownQuery.data?.nameservers, debouncedSearch])

  // Virtualized list for performance with large datasets
  const virtualizer = useVirtualizer({
    count: filteredData.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 48, // Height of each row
    overscan: 5, // Render 5 extra items for smooth scrolling
  })

  // Reset search when modal opens/closes
  useEffect(() => {
    if (!open) setSearchQuery('')
  }, [open])

  // Handle keyboard shortcuts
  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Escape') onClose()
  }, [onClose])

  if (!open || !provider) return null

  const virtualItems = virtualizer.getVirtualItems()

  return (
    <div className="modal-backdrop" onClick={onClose} onKeyDown={handleKeyDown}>
      <section className="modal-panel enhanced" onClick={(e) => e.stopPropagation()}>
        <header className="modal-header">
          <div className="header-content">
            <h3>{provider} nameservers</h3>
            {breakdownQuery.data && !breakdownQuery.isFetching && (
              <p className="stats-line">
                {formatNumber(breakdownQuery.data.total_nameservers)} unique NS •{' '}
                {formatNumber(breakdownQuery.data.total_domains)} domains
                {breakdownQuery.data.cached && (
                  <span className="cached-badge">cached</span>
                )}
              </p>
            )}
            {breakdownQuery.isFetching && (
              <p className="stats-line updating">
                <Loader2 size={12} className="spinner-small" /> Updating...
              </p>
            )}
          </div>
          <div className="header-actions">
            {breakdownQuery.data && (
              <button
                type="button"
                className="icon-btn export-btn"
                onClick={() => exportToCSV(breakdownQuery.data!.nameservers, provider)}
                title="Export to CSV"
              >
                <Download size={16} />
              </button>
            )}
            <button type="button" className="icon-btn" onClick={onClose} aria-label="Close modal">
              <X size={16} />
            </button>
          </div>
        </header>

        {/* Search bar */}
        <div className="search-bar">
          <Search size={16} className="search-icon" />
          <input
            type="text"
            placeholder="Search nameservers..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="search-input"
          />
          {searchQuery && (
            <span className="search-results">{formatNumber(filteredData.length)} results</span>
          )}
        </div>

        {/* Skeleton loading state */}
        {breakdownQuery.isLoading && !breakdownQuery.data && (
          <div className="skeleton-container">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </div>
        )}

        {/* Error state */}
        {breakdownQuery.isError && (
          <div className="modal-state error">
            <AlertCircle size={18} />
            <span>{(breakdownQuery.error as Error).message || 'Failed to load breakdown.'}</span>
          </div>
        )}

        {/* Empty state */}
        {breakdownQuery.data && filteredData.length === 0 && (
          <div className="modal-state">
            <AlertCircle size={18} />
            <span>
              {searchQuery
                ? 'No nameservers match your search.'
                : 'No nameservers found for this provider.'}
            </span>
          </div>
        )}

        {/* Virtualized list */}
        {breakdownQuery.data && filteredData.length > 0 && (
          <div ref={parentRef} className="virtual-list-container">
            <div
              style={{
                height: `${virtualizer.getTotalSize()}px`,
                width: '100%',
                position: 'relative',
              }}
            >
              {virtualItems.map((virtualItem) => {
                const item = filteredData[virtualItem.index]
                return (
                  <div
                    key={item.nameserver}
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      height: `${virtualItem.size}px`,
                      transform: `translateY(${virtualItem.start}px)`,
                    }}
                    className="ns-list-item"
                  >
                    <span className="ns-name" title={item.nameserver}>
                      {item.nameserver}
                    </span>
                    <div className="ns-row-actions">
                      <strong className="ns-count">{formatNumber(item.count)}</strong>
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
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Footer with progress info */}
        {breakdownQuery.data && (
          <footer className="modal-footer">
            <span>
              Showing {formatNumber(filteredData.length)} of{' '}
              {formatNumber(breakdownQuery.data.total_nameservers)} nameservers
            </span>
            {breakdownQuery.isFetching && <span className="refresh-indicator">Refreshing...</span>}
          </footer>
        )}
      </section>
    </div>
  )
}
