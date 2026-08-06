/**
 * API client for the Ruminate backend.
 * In dev, Vite proxies /api → http://127.0.0.1:8420.
 */

const API_BASE = '/api'

// --- types ---

export type AskMode = 'agent' | 'rag'

export interface Ref {
  title: string
  path: string
  layer?: string
  snippet?: string
}

export interface Step {
  index: number
  thought?: string
  action?: string
  args?: Record<string, unknown>
  observation?: string
  obs_bytes?: number
  duration_ms: number
  final?: boolean
  final_answer?: string
  prompt_tokens?: number
  completion_tokens?: number
}

export interface AskRequest {
  question: string
  wiki?: string
  mode?: AskMode
  max_steps?: number
  top_n?: number
  effort?: string
  evidence?: string
}

export interface AskResponse {
  answer: string
  refs?: Ref[]
  steps?: Step[]
  truncated?: boolean
  mode: AskMode
}

export interface StreamEvent {
  type: 'progress' | 'step' | 'content' | 'done' | 'error'
  phase?: string
  step?: number
  action?: string
  args?: Record<string, unknown>
  step_data?: Step
  content?: string
  answer?: string
  refs?: Ref[]
  truncated?: boolean
  mode?: AskMode
  error?: string
}

export interface HealthResponse {
  status: string
  wiki?: string
  wikis?: string[]
  default?: string
  fixed?: string
  multi?: boolean
}

export interface WikiInfo {
  name: string
}

export interface WikisResponse {
  wikis: WikiInfo[]
  default: string
  fixed?: string
  multi: boolean
}

export interface Topic {
  title: string
  type: string
}

export interface RecentActivity {
  date: string
  operation: string
  page_type: string
  title: string
}

export interface WikiStats {
  wiki: string
  summaries: number
  entities: number
  concepts: number
  synthesis: number
  pages: number
  sources: number
  links: number
  topics?: Topic[]
  recent?: RecentActivity[]
  updated: string
}

// --- low-level ---

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(options?.headers ?? {}),
    },
    ...options,
  })
  if (!res.ok) {
    let detail = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body?.error) detail = body.error
    } catch {
      /* ignore */
    }
    throw new Error(detail)
  }
  return res.json()
}

// --- public API ---

export function healthCheck(wiki?: string) {
  const q = wiki ? `?wiki=${encodeURIComponent(wiki)}` : ''
  return request<HealthResponse>(`/health${q}`)
}

export function fetchWikis() {
  return request<WikisResponse>('/wikis')
}

export function fetchStats(wiki?: string) {
  const q = wiki ? `?wiki=${encodeURIComponent(wiki)}` : ''
  return request<WikiStats>(`/stats${q}`)
}

export function ask(req: AskRequest) {
  return request<AskResponse>('/ask', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

/**
 * Stream an ask via SSE (POST /api/ask/stream).
 * Calls onEvent for each parsed StreamEvent until done/error or abort.
 */
export async function askStream(
  req: AskRequest,
  onEvent: (ev: StreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/ask/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
    },
    body: JSON.stringify(req),
    signal,
  })

  if (!res.ok) {
    let detail = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body?.error) detail = body.error
    } catch {
      /* ignore */
    }
    throw new Error(detail)
  }

  if (!res.body) {
    throw new Error('streaming body not available')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    // SSE frames are separated by blank lines.
    let sep: number
    while ((sep = buffer.indexOf('\n\n')) >= 0) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      for (const line of frame.split('\n')) {
        if (!line.startsWith('data: ')) continue
        const payload = line.slice(6).trim()
        if (!payload) continue
        try {
          const ev = JSON.parse(payload) as StreamEvent
          onEvent(ev)
          if (ev.type === 'done' || ev.type === 'error') {
            return
          }
        } catch {
          // skip malformed frames
        }
      }
    }
  }
}
