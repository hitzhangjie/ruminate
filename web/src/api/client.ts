/**
 * API client skeleton for communicating with the Ruminate backend.
 * All requests are proxied through Vite's dev server to the Go backend.
 */

const API_BASE = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
    },
    ...options,
  })
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`)
  }
  return res.json()
}

// Health check
export function healthCheck() {
  return request<{ status: string }>('/health')
}

// Wiki pages
export function listPages() {
  return request<unknown[]>('/wiki/pages')
}

export function getPage(id: string) {
  return request<unknown>(`/wiki/pages/${id}`)
}

// Search
export function search(query: string) {
  return request<unknown[]>(`/search?q=${encodeURIComponent(query)}`)
}

// AI ask
export function ask(question: string) {
  return request<unknown>('/ask', {
    method: 'POST',
    body: JSON.stringify({ question }),
  })
}

// Ingest
export function ingest(content: string) {
  return request<unknown>('/ingest', {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}
