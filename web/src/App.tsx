import { BrowserRouter, Routes, Route } from 'react-router-dom'
import WikiBrowse from './pages/WikiBrowse'
import AIChat from './pages/AIChat'
import IngestManage from './pages/IngestManage'
import GraphView from './pages/GraphView'

function Home() {
  return (
    <div className="home">
      <h1>Ruminate</h1>
      <p>AI-driven personal knowledge base</p>
      <nav>
        <ul>
          <li><a href="/wiki">Wiki Browse</a></li>
          <li><a href="/chat">AI Chat</a></li>
          <li><a href="/ingest">Ingest</a></li>
          <li><a href="/graph">Knowledge Graph</a></li>
        </ul>
      </nav>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/wiki" element={<WikiBrowse />} />
        <Route path="/wiki/:page" element={<WikiBrowse />} />
        <Route path="/chat" element={<AIChat />} />
        <Route path="/ingest" element={<IngestManage />} />
        <Route path="/graph" element={<GraphView />} />
      </Routes>
    </BrowserRouter>
  )
}
