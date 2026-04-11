import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Upload, FileText, CheckCircle, AlertCircle, Loader2, Trash2, Database, Pause, Play, X } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { uploadCSV, getUploadStatus, getUploadErrors, cleanDuplicates, getDuplicateStats } from '../api'
import type { UploadStatus, FailedRow } from '../types'

interface FileUpload {
  file: File
  id: string
  status: UploadStatus | null
  error: string | null
  failedRows: FailedRow[]
  failedCount: number
  paused: boolean
  abortController?: AbortController
}

export default function UploadPage() {
  const navigate = useNavigate()
  const [uploads, setUploads] = useState<FileUpload[]>([])
  const pausedUploads = useRef<Set<string>>(new Set())
  const [isDragging, setIsDragging] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const pollingRefs = useRef<Map<string, number>>(new Map())

  const startPolling = useCallback((uploadId: string, filename: string) => {
    if (pollingRefs.current.has(filename)) return

    const intervalId = window.setInterval(async () => {
      // Skip if paused
      if (pausedUploads.current.has(uploadId)) {
        return
      }

      try {
        const status = await getUploadStatus(filename)
        setUploads((prev) =>
          prev.map((u) =>
            u.file.name === filename ? { ...u, status } : u
          )
        )

        // Stop polling if completed or error
        if (status.status === 'completed' || status.status === 'error') {
          const id = pollingRefs.current.get(filename)
          if (id) {
            clearInterval(id)
            pollingRefs.current.delete(filename)
          }
        }
      } catch (err) {
        console.error('Failed to get status:', err)
      }
    }, 500) // Poll every 500ms

    pollingRefs.current.set(filename, intervalId)
  }, [])

  const stopAllPolling = useCallback(() => {
    pollingRefs.current.forEach((id) => clearInterval(id))
    pollingRefs.current.clear()
  }, [])

  useEffect(() => {
    return () => {
      stopAllPolling()
    }
  }, [stopAllPolling])

  const handleFileSelect = useCallback(async (files: FileList | null) => {
    if (!files || files.length === 0) return

    const csvFiles = Array.from(files).filter(
      (f) => f.name.endsWith('.csv') || f.type === 'text/csv'
    )

    if (csvFiles.length === 0) {
      alert('Please select CSV files only')
      return
    }

    // Add files to upload list
    const newUploads: FileUpload[] = csvFiles.map((file) => ({
      file,
      id: `${file.name}-${Date.now()}`,
      status: {
        filename: file.name,
        status: 'uploading',
        total_rows: 0,
        processed_rows: 0,
        inserted_rows: 0,
        errors: 0,
        failed_rows: 0,
        current_batch: 0,
        start_time: new Date().toISOString(),
        elapsed_seconds: 0,
        message: 'Starting upload...',
      },
      error: null,
      failedRows: [],
      failedCount: 0,
      paused: false,
      abortController: new AbortController(),
    }))

    setUploads((prev) => [...prev, ...newUploads])

    // Upload files one by one (no bulk)
    for (const upload of newUploads) {
      try {
        const response = await uploadCSV(upload.file, upload.abortController?.signal)
        console.log('Upload started:', response)
        startPolling(upload.id, upload.file.name)
      } catch (err) {
        setUploads((prev) =>
          prev.map((u) =>
            u.id === upload.id
              ? { ...u, error: err instanceof Error ? err.message : 'Upload failed' }
              : u
          )
        )
      }
    }
  }, [startPolling])

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      setIsDragging(false)
      handleFileSelect(e.dataTransfer.files)
    },
    [handleFileSelect]
  )

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }, [])

  const onDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
  }, [])

  const formatFileSize = (bytes: number): string => {
    if (bytes === 0) return '0 Bytes'
    const k = 1024
    const sizes = ['Bytes', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  const formatDuration = (seconds: number): string => {
    if (seconds < 60) return `${seconds.toFixed(1)}s`
    const mins = Math.floor(seconds / 60)
    const secs = Math.floor(seconds % 60)
    return `${mins}m ${secs}s`
  }

  const getStatusIcon = (status: UploadStatus | null) => {
    if (!status) return <Loader2 size={20} className="spinner" />
    switch (status.status) {
      case 'completed':
        return <CheckCircle size={20} color="#22c55e" />
      case 'error':
        return <AlertCircle size={20} color="#ef4444" />
      case 'processing':
      case 'uploading':
        return <Loader2 size={20} className="spinner" />
      default:
        return <Loader2 size={20} className="spinner" />
    }
  }

  const getStatusClass = (status: UploadStatus | null): string => {
    if (!status) return 'upload-item'
    switch (status.status) {
      case 'completed':
        return 'upload-item completed'
      case 'error':
        return 'upload-item error'
      case 'processing':
        return 'upload-item processing'
      default:
        return 'upload-item'
    }
  }

  return (
    <div className="upload-page">
      <header className="upload-header">
        <button onClick={() => navigate('/')} className="back-button">
          <ArrowLeft size={20} />
          Back to Lookup
        </button>
        <h1>Upload CSV Files</h1>
        <p className="subtitle">Upload domain CSV files (100-250MB). Files are processed one by one.</p>
      </header>

      <section
        className={`upload-dropzone ${isDragging ? 'dragging' : ''}`}
        onDrop={onDrop}
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onClick={() => fileInputRef.current?.click()}
      >
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,text/csv"
          multiple
          onChange={(e) => handleFileSelect(e.target.files)}
          style={{ display: 'none' }}
        />
        <Upload size={48} className="upload-icon" />
        <p className="upload-text">
          <strong>Click to select</strong> or drag and drop CSV files
        </p>
        <p className="upload-hint">Supports files up to 250MB. Format: domain,level,ns</p>
      </section>

      {uploads.length > 0 && (
        <section className="upload-list">
          <h2>Upload Progress</h2>
          {uploads.map((upload) => (
            <div key={upload.id} className={getStatusClass(upload.status)}>
              <div className="upload-item-header">
                <div className="upload-item-icon">
                  <FileText size={24} />
                </div>
                <div className="upload-item-info">
                  <h3>{upload.file.name}</h3>
                  <span className="file-size">{formatFileSize(upload.file.size)}</span>
                </div>
                <div className="upload-item-status">{getStatusIcon(upload.status)}</div>
                <div className="upload-item-controls">
                  {(upload.status?.status === 'uploading' || upload.status?.status === 'processing') && (
                    <>
                      <button
                        className="control-btn pause-btn"
                        onClick={(e) => {
                          e.stopPropagation()
                          if (upload.paused) {
                            pausedUploads.current.delete(upload.id)
                            setUploads(prev => prev.map(u => u.id === upload.id ? { ...u, paused: false } : u))
                          } else {
                            pausedUploads.current.add(upload.id)
                            setUploads(prev => prev.map(u => u.id === upload.id ? { ...u, paused: true } : u))
                          }
                        }}
                        title={upload.paused ? 'Resume' : 'Pause'}
                      >
                        {upload.paused ? <Play size={16} /> : <Pause size={16} />}
                      </button>
                      <button
                        className="control-btn cancel-btn"
                        onClick={(e) => {
                          e.stopPropagation()
                          if (confirm('Cancel this upload?')) {
                            upload.abortController?.abort()
                            const id = pollingRefs.current.get(upload.file.name)
                            if (id) {
                              clearInterval(id)
                              pollingRefs.current.delete(upload.file.name)
                            }
                            setUploads(prev => prev.filter(u => u.id !== upload.id))
                          }
                        }}
                        title="Cancel"
                      >
                        <X size={16} />
                      </button>
                    </>
                  )}
                </div>
              </div>

              {upload.error && (
                <div className="upload-error">
                  <AlertCircle size={16} />
                  {upload.error}
                </div>
              )}

              {upload.status && (
                <div className="upload-progress">
                  <div className="progress-bar">
                    <div
                      className="progress-fill"
                      style={{
                        width: `${upload.status.total_rows > 0 ? (upload.status.processed_rows / upload.status.total_rows) * 100 : 0}%`,
                      }}
                    />
                  </div>

                  <div className="upload-stats">
                    <div className="stat">
                      <span className="stat-label">Status</span>
                      <span className={`stat-value status-${upload.status.status}`}>
                        {upload.status.status}
                      </span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Rows</span>
                      <span className="stat-value">{upload.status.processed_rows.toLocaleString()}</span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Inserted</span>
                      <span className="stat-value">{upload.status.inserted_rows.toLocaleString()}</span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Errors</span>
                      <span className={`stat-value ${upload.status.errors > 0 ? 'has-errors' : ''}`}>
                        {upload.status.errors.toLocaleString()}
                      </span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Failed Rows</span>
                      <span className={`stat-value ${upload.failedCount > 0 ? 'has-errors' : ''}`}>
                        {upload.failedCount.toLocaleString()}
                      </span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Batches</span>
                      <span className="stat-value">{upload.status.current_batch}</span>
                    </div>
                    <div className="stat">
                      <span className="stat-label">Time</span>
                      <span className="stat-value">{formatDuration(upload.status.elapsed_seconds)}</span>
                    </div>
                  </div>

                  <pre className="upload-log">{upload.status.message || 'Waiting...'}</pre>

                  {upload.failedRows.length > 0 && (
                    <div className="failed-rows">
                      <div className="failed-header">
                        <strong>Failed rows (showing {upload.failedRows.length}/{upload.failedCount || upload.failedRows.length})</strong>
                        <button
                          type="button"
                          className="secondary"
                          onClick={async () => {
                            try {
                              const data = await getUploadErrors(upload.file.name)
                              setUploads((prev) =>
                                prev.map((u) =>
                                  u.id === upload.id
                                    ? {
                                        ...u,
                                        failedRows: data.failed_rows,
                                        failedCount: data.count,
                                      }
                                    : u,
                                ),
                              )
                            } catch (err) {
                              console.error('Failed to fetch upload errors', err)
                            }
                          }}
                        >
                          Refresh
                        </button>
                      </div>
                      <ul>
                        {upload.failedRows.map((row) => (
                          <li key={`${upload.id}-${row.line}`}>
                            <span className="failed-line">Line {row.line}</span>
                            <span className="failed-reason">{row.reason}</span>
                            <pre className="failed-raw">{row.raw}</pre>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </section>
      )}

      {uploads.length === 0 && (
        <section className="upload-help">
          <h3>Expected CSV Format</h3>
          <pre className="format-example">
{`domain,level,ns
google.com,10,ns1.google.com,ns2.google.com
facebook.com,9,ns1.facebook.com
...`}
          </pre>
          <ul>
            <li>Column 1: Domain name</li>
            <li>Column 2: Domain level/rank</li>
            <li>Column 3+: Nameservers (comma-separated within cell)</li>
          </ul>
        </section>
      )}

      {/* Deduplication Section */}
      <section className="deduplication-section">
        <h3>
          <Database size={20} />
          Database Maintenance
        </h3>
        <p className="dedup-description">
          Remove duplicate domain entries to optimize database size and query performance.
        </p>
        <div className="dedup-actions">
          <button
            className="dedup-stats-btn"
            onClick={async () => {
              try {
                const stats = await getDuplicateStats()
                alert(`Duplicate Stats:\nLegacy table: ${stats.legacy_table_duplicates} duplicate groups\nSharded table (sample): ${stats.sharded_table_bucket_sample} duplicate groups\nEstimated total duplicates: ${stats.sharded_table_estimated}`)
              } catch (err) {
                alert('Failed to get duplicate stats: ' + (err instanceof Error ? err.message : 'Unknown error'))
              }
            }}
          >
            <Database size={16} />
            Check Duplicates
          </button>
          <button
            className="dedup-clean-btn"
            onClick={async () => {
              if (!confirm('This will remove all duplicate domain entries. Continue?')) {
                return
              }
              try {
                const result = await cleanDuplicates()
                alert(`Deduplication complete!\nRemoved: ${result.duplicates_removed.toLocaleString()} duplicates\nRemaining domains: ${result.total_domains.toLocaleString()}\nTime: ${result.time_taken}`)
              } catch (err) {
                alert('Deduplication failed: ' + (err instanceof Error ? err.message : 'Unknown error'))
              }
            }}
          >
            <Trash2 size={16} />
            Clean Duplicates
          </button>
        </div>
      </section>
    </div>
  )
}
