import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { fetchWikis, type WikisResponse } from '../api/client'

const STORAGE_KEY = 'ruminate.selectedWiki'

interface WikiContextValue {
  /** Currently selected wiki name (ready for API calls). */
  wiki: string
  /** All available wiki names. */
  wikis: string[]
  defaultWiki: string
  /** Server locked with --wiki (selector disabled). */
  fixed: boolean
  /** True when the UI should show a wiki switcher. */
  multi: boolean
  loading: boolean
  error: string | null
  setWiki: (name: string) => void
}

const WikiContext = createContext<WikiContextValue | null>(null)

export function WikiProvider({ children }: { children: ReactNode }) {
  const [catalog, setCatalog] = useState<WikisResponse | null>(null)
  const [wiki, setWikiState] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchWikis()
      .then((res) => {
        if (cancelled) return
        setCatalog(res)
        const names = res.wikis.map((w) => w.name)
        const stored = localStorage.getItem(STORAGE_KEY) || ''
        let initial = res.default
        if (res.fixed) {
          initial = res.fixed
        } else if (stored && names.includes(stored)) {
          initial = stored
        } else if (!names.includes(initial) && names.length > 0) {
          initial = names[0]
        }
        setWikiState(initial)
        setError(null)
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const setWiki = useCallback(
    (name: string) => {
      if (!catalog) return
      if (catalog.fixed) return
      const names = catalog.wikis.map((w) => w.name)
      if (!names.includes(name)) return
      setWikiState(name)
      localStorage.setItem(STORAGE_KEY, name)
    },
    [catalog],
  )

  const value = useMemo<WikiContextValue>(() => {
    const names = catalog?.wikis.map((w) => w.name) ?? []
    return {
      wiki,
      wikis: names,
      defaultWiki: catalog?.default ?? '',
      fixed: Boolean(catalog?.fixed),
      multi: Boolean(catalog?.multi),
      loading,
      error,
      setWiki,
    }
  }, [catalog, wiki, loading, error, setWiki])

  return <WikiContext.Provider value={value}>{children}</WikiContext.Provider>
}

export function useWiki(): WikiContextValue {
  const ctx = useContext(WikiContext)
  if (!ctx) {
    throw new Error('useWiki must be used within WikiProvider')
  }
  return ctx
}
