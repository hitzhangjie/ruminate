import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import {
  askStream,
  healthCheck,
  type AskMode,
  type Ref,
  type Step,
  type StreamEvent,
} from '../api/client'
import { useWiki } from '../wiki/WikiContext'

interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  mode?: AskMode
  refs?: Ref[]
  steps?: Step[]
  progress?: string
  truncated?: boolean
  error?: string
  streaming?: boolean
}

function formatActionDetail(action?: string, args?: Record<string, unknown>): string {
  if (!action) return ''
  if (!args) return action
  const pick = (...keys: string[]) => {
    for (const k of keys) {
      const v = args[k]
      if (typeof v === 'string' && v) return v
      if (typeof v === 'number') return String(v)
    }
    return ''
  }
  switch (action) {
    case 'wiki_search':
    case 'raw_search':
      return `${action} · "${pick('query')}"`
    case 'wiki_read':
    case 'raw_read':
    case 'file_read':
    case 'list_dir':
      return `${action} · ${pick('path')}`
    case 'file_grep':
      return `${action} · "${pick('pattern')}"`
    case 'symbol_search':
      return `${action} · ${pick('name')}`
    default:
      return action
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

let idSeq = 0
function nextId() {
  idSeq += 1
  return `m-${Date.now()}-${idSeq}`
}

export default function AIChat() {
  const { wiki, loading: wikiLoading } = useWiki()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [mode, setMode] = useState<AskMode>('agent')
  const [busy, setBusy] = useState(false)
  const [health, setHealth] = useState<string>('checking…')
  const abortRef = useRef<AbortController | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (wikiLoading || !wiki) return
    healthCheck(wiki)
      .then((h) => setHealth(h.wiki ? `connected · wiki=${h.wiki}` : `connected · wiki=${wiki}`))
      .catch((e: Error) => setHealth(`offline · ${e.message}`))
  }, [wiki, wikiLoading])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const updateAssistant = useCallback((id: string, patch: Partial<ChatMessage>) => {
    setMessages((prev) =>
      prev.map((m) => (m.id === id ? { ...m, ...patch } : m)),
    )
  }, [])

  const handleEvent = useCallback(
    (assistantId: string, ev: StreamEvent) => {
      setMessages((prev) => {
        const idx = prev.findIndex((m) => m.id === assistantId)
        if (idx < 0) return prev
        const cur = { ...prev[idx] }
        const next = [...prev]

        switch (ev.type) {
          case 'progress': {
            if (ev.phase === 'decide') {
              cur.progress = `Thinking… (step ${ev.step ?? '?'})`
            } else if (ev.phase === 'tool') {
              cur.progress = formatActionDetail(ev.action, ev.args) || `Running tool…`
            }
            break
          }
          case 'step': {
            const step = ev.step_data
            if (step) {
              cur.steps = [...(cur.steps ?? []), step]
            }
            cur.progress = undefined
            break
          }
          case 'content': {
            cur.content = (cur.content || '') + (ev.content || '')
            cur.progress = undefined
            break
          }
          case 'done': {
            cur.content = ev.answer ?? cur.content
            cur.refs = ev.refs
            cur.truncated = ev.truncated
            cur.mode = ev.mode
            cur.streaming = false
            cur.progress = undefined
            break
          }
          case 'error': {
            cur.error = ev.error || 'unknown error'
            cur.streaming = false
            cur.progress = undefined
            break
          }
        }

        next[idx] = cur
        return next
      })
    },
    [],
  )

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    const question = input.trim()
    if (!question || busy) return

    setInput('')
    setBusy(true)

    const userMsg: ChatMessage = { id: nextId(), role: 'user', content: question }
    const assistantId = nextId()
    const assistantMsg: ChatMessage = {
      id: assistantId,
      role: 'assistant',
      content: '',
      mode,
      steps: [],
      streaming: true,
      progress: mode === 'agent' ? 'Starting agent…' : 'Retrieving…',
    }
    setMessages((prev) => [...prev, userMsg, assistantMsg])

    const ac = new AbortController()
    abortRef.current = ac

    try {
      await askStream(
        { question, mode, wiki },
        (ev) => handleEvent(assistantId, ev),
        ac.signal,
      )
      // If stream ended without done, clear streaming flag.
      updateAssistant(assistantId, { streaming: false, progress: undefined })
    } catch (err) {
      if ((err as Error).name === 'AbortError') {
        updateAssistant(assistantId, {
          streaming: false,
          progress: undefined,
          error: 'cancelled',
        })
      } else {
        updateAssistant(assistantId, {
          streaming: false,
          progress: undefined,
          error: (err as Error).message,
        })
      }
    } finally {
      abortRef.current = null
      setBusy(false)
    }
  }

  function onStop() {
    abortRef.current?.abort()
  }

  return (
    <div className="chat-page">
      <header className="chat-header">
        <div>
          <h1>AI Chat</h1>
          <p className="chat-status">{health}</p>
        </div>
        <label className="mode-picker">
          Mode
          <select
            value={mode}
            disabled={busy}
            onChange={(e) => setMode(e.target.value as AskMode)}
          >
            <option value="agent">agent (ReAct)</option>
            <option value="rag">rag (single-pass)</option>
          </select>
        </label>
      </header>

      <div className="chat-messages">
        {messages.length === 0 && (
          <div className="chat-empty">
            <p>Ask anything against your wiki.</p>
            <p className="muted">
              Default mode is multi-step agent exploration; switch to rag for classic retrieve → generate.
            </p>
          </div>
        )}

        {messages.map((m) => (
          <div key={m.id} className={`msg msg-${m.role}`}>
            <div className="msg-role">{m.role === 'user' ? 'You' : 'Ruminate'}</div>

            {m.role === 'assistant' && m.steps && m.steps.length > 0 && (
              <div className="step-timeline">
                {m.steps.map((s) => (
                  <details key={s.index} className={`step-card ${s.final ? 'final' : ''}`}>
                    <summary>
                      <span className="step-mark">{s.final ? '→' : '●'}</span>
                      <span className="step-label">
                        {s.final
                          ? 'final_answer'
                          : formatActionDetail(s.action, s.args) || s.action || `step ${s.index}`}
                      </span>
                      <span className="step-meta">{formatDuration(s.duration_ms)}</span>
                    </summary>
                    {s.thought && s.thought !== 'parse_error' && (
                      <pre className="step-body">{s.thought}</pre>
                    )}
                    {s.observation && (
                      <pre className="step-body obs">{s.observation.slice(0, 2000)}</pre>
                    )}
                  </details>
                ))}
              </div>
            )}

            {m.progress && (
              <div className="msg-progress">
                <span className="spinner" /> {m.progress}
              </div>
            )}

            {m.content && <div className="msg-content">{m.content}</div>}

            {m.error && <div className="msg-error">{m.error}</div>}

            {m.refs && m.refs.length > 0 && (
              <div className="msg-refs">
                <div className="refs-title">References</div>
                <ul>
                  {m.refs.map((r, i) => (
                    <li key={`${r.path}-${i}`}>
                      <span className="ref-layer">[{r.layer || 'wiki'}]</span>{' '}
                      {r.title} <span className="muted">({r.path})</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {m.truncated && (
              <div className="msg-note">Agent stopped due to step/time budget.</div>
            )}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      <form className="chat-input" onSubmit={onSubmit}>
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Ask a question…"
          rows={2}
          disabled={busy}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              onSubmit(e)
            }
          }}
        />
        <div className="chat-actions">
          {busy ? (
            <button type="button" className="btn-secondary" onClick={onStop}>
              Stop
            </button>
          ) : (
            <button type="submit" className="btn-primary" disabled={!input.trim()}>
              Ask
            </button>
          )}
        </div>
      </form>
    </div>
  )
}
