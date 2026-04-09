import type {
  AllDomainsResponse,
  ReverseNSResponse,
  HostingProvidersResponse,
  ProviderNSBreakdownResponse,
  UploadStatus,
  UploadErrorsResponse,
  GlobalStatsResponse,
} from './types'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? ''
const PROVIDERS_CACHE_KEY = 'revns:hosting-providers:all'
const PROVIDERS_CACHE_TTL_MS = 5 * 60 * 1000

async function request<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, { signal })

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`
    try {
      const body = (await response.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Ignore JSON parsing errors and keep fallback message.
    }
    throw new Error(message)
  }

  return (await response.json()) as T
}

export function getReverseNS(nameserver: string, page: number, limit: number, signal?: AbortSignal) {
  return request<ReverseNSResponse>(
    `/api/v1/ns/${encodeURIComponent(nameserver)}?page=${page}&limit=${limit}`,
    signal,
  )
}

export function getAllDomains(nameserver: string, signal?: AbortSignal) {
  return request<AllDomainsResponse>(`/api/v1/ns/${encodeURIComponent(nameserver)}/all`, signal)
}

export function getTopHostingProviders(signal?: AbortSignal) {
  return request<HostingProvidersResponse>('/api/v1/hosting-providers/top', signal)
}

export function getAllHostingProviders(signal?: AbortSignal) {
  try {
    const cachedRaw = localStorage.getItem(PROVIDERS_CACHE_KEY)
    if (cachedRaw) {
      const cached = JSON.parse(cachedRaw) as { data: HostingProvidersResponse; ts: number }
      if (Date.now() - cached.ts < PROVIDERS_CACHE_TTL_MS) {
        return Promise.resolve(cached.data)
      }
    }
  } catch {
    // Ignore cache read errors and continue with network request.
  }

  return request<HostingProvidersResponse>('/api/v1/hosting-providers', signal).then((data) => {
    try {
      localStorage.setItem(PROVIDERS_CACHE_KEY, JSON.stringify({ data, ts: Date.now() }))
    } catch {
      // Ignore cache write errors.
    }
    return data
  })
}

export function getHostingProvidersPage(page: number, limit: number, signal?: AbortSignal) {
  return request<HostingProvidersResponse>(`/api/v1/hosting-providers?page=${page}&limit=${limit}`, signal)
}

export function getProviderNSBreakdown(provider: string, signal?: AbortSignal) {
  return request<ProviderNSBreakdownResponse>(
    `/api/v1/hosting-providers/${encodeURIComponent(provider)}/ns`,
    signal,
  )
}

export function uploadCSV(file: File): Promise<{ message: string; filename: string; status_url: string }> {
  const formData = new FormData()
  formData.append('file', file)

  return fetch(`${API_BASE_URL}/api/v1/upload`, {
    method: 'POST',
    body: formData,
  }).then(async (response) => {
    if (!response.ok) {
      const body = await response.json().catch(() => ({ error: 'Upload failed' }))
      throw new Error(body.error || `Upload failed with status ${response.status}`)
    }
    return response.json()
  })
}

export function getUploadStatus(filename: string, signal?: AbortSignal) {
  return request<UploadStatus>(`/api/v1/upload/status?filename=${encodeURIComponent(filename)}`, signal)
}

export function getUploadErrors(filename: string, signal?: AbortSignal) {
  return request<UploadErrorsResponse>(`/api/v1/upload/errors?filename=${encodeURIComponent(filename)}`, signal)
}

export function getGlobalStats(signal?: AbortSignal) {
  return request<GlobalStatsResponse>('/api/v1/stats', signal)
}
