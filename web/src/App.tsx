import type { ReactNode } from 'react'
import { BrowserRouter, NavLink, Routes, Route } from 'react-router-dom'
import Home from './pages/Home'
import WikiBrowse from './pages/WikiBrowse'
import AIChat from './pages/AIChat'
import IngestManage from './pages/IngestManage'
import GraphView from './pages/GraphView'
import { WikiProvider, useWiki } from './wiki/WikiContext'

function WikiSwitcher() {
  const { wiki, wikis, multi, fixed, loading, setWiki } = useWiki()
  if (loading) {
    return <span className="wiki-switcher muted">…</span>
  }
  if (fixed || !multi || wikis.length <= 1) {
    return (
      <span className="wiki-switcher wiki-locked" title={fixed ? '锁定于 --wiki' : '当前知识库'}>
        <span className="wiki-switcher-label">wiki</span>
        <span className="wiki-switcher-name">{wiki || '—'}</span>
      </span>
    )
  }
  return (
    <label className="wiki-switcher">
      <span className="wiki-switcher-label">wiki</span>
      <select value={wiki} onChange={(e) => setWiki(e.target.value)} aria-label="选择知识库">
        {wikis.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </select>
    </label>
  )
}

function Layout({ children }: { children: ReactNode }) {
  return (
    <div className="app-shell">
      <nav className="top-nav">
        <NavLink to="/" className="brand" end>
          Ruminate
        </NavLink>
        <div className="nav-links">
          <NavLink to="/chat">Chat</NavLink>
          <NavLink to="/wiki" className="nav-muted">
            Wiki
          </NavLink>
          <NavLink to="/ingest" className="nav-muted">
            Ingest
          </NavLink>
          <NavLink to="/graph" className="nav-muted">
            Graph
          </NavLink>
          <WikiSwitcher />
        </div>
      </nav>
      <main className="app-main">{children}</main>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <WikiProvider>
        <Layout>
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/wiki" element={<WikiBrowse />} />
            <Route path="/wiki/:page" element={<WikiBrowse />} />
            <Route path="/chat" element={<AIChat />} />
            <Route path="/ingest" element={<IngestManage />} />
            <Route path="/graph" element={<GraphView />} />
          </Routes>
        </Layout>
      </WikiProvider>
    </BrowserRouter>
  )
}
