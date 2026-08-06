import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { fetchStats, type WikiStats } from '../api/client'
import { useWiki } from '../wiki/WikiContext'

function typeLabel(t: string): string {
  switch (t) {
    case 'summaries':
      return '摘要'
    case 'entities':
      return '实体'
    case 'concepts':
      return '概念'
    case 'synthesis':
      return '综合'
    default:
      return t
  }
}

function opLabel(op: string): string {
  switch (op) {
    case 'create':
      return '新建'
    case 'update':
      return '更新'
    case 'delete':
      return '删除'
    default:
      return op
  }
}

export default function Home() {
  const { wiki, multi, loading: wikiLoading, error: wikiError } = useWiki()
  const [stats, setStats] = useState<WikiStats | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (wikiLoading || !wiki) return
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchStats(wiki)
      .then((s) => {
        if (!cancelled) setStats(s)
      })
      .catch((e: Error) => {
        if (!cancelled) {
          setStats(null)
          setError(e.message)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [wiki, wikiLoading])

  return (
    <div className="home-brain">
      <div className="brain-glow" aria-hidden />
      <div className="brain-dots" aria-hidden />

      <header className="brain-hero">
        <div className="brain-mark" aria-hidden>
          <span className="brain-core" />
          <span className="brain-ring r1" />
          <span className="brain-ring r2" />
        </div>
        <div className="brain-hero-text">
          <p className="brain-kicker">Personal knowledge mind</p>
          <h1>
            {stats?.wiki ? (
              <>
                <span className="wiki-name">{stats.wiki}</span>
                <span className="wiki-suffix"> · Ruminate</span>
              </>
            ) : (
              'Ruminate'
            )}
          </h1>
          <p className="brain-lede">
            知识在此被消化、交织、生长——不是一堆文件，而是你可以信任、可以追问的思维网络。
            {multi && (
              <span className="brain-lede-extra">
                {' '}
                顶部可切换不同知识库；下方统计始终对应当前选中的大脑。
              </span>
            )}
          </p>
        </div>
      </header>

      {(loading || wikiLoading) && (
        <div className="brain-loading">
          <span className="spinner" /> 正在唤醒知识库…
        </div>
      )}

      {(error || wikiError) && (
        <div className="brain-error">
          无法读取知识库状态：{error || wikiError}
        </div>
      )}

      {stats && !loading && (
        <>
          <section className="brain-pulse" aria-label="知识体量">
            <div className="pulse-stat">
              <div className="pulse-num">{stats.pages}</div>
              <div className="pulse-label">Wiki 页面</div>
            </div>
            <div className="pulse-divider" />
            <div className="pulse-stat">
              <div className="pulse-num">{stats.sources}</div>
              <div className="pulse-label">已摄入源</div>
            </div>
            <div className="pulse-divider" />
            <div className="pulse-stat">
              <div className="pulse-num">{stats.links}</div>
              <div className="pulse-label">交叉链接</div>
            </div>
          </section>

          <section className="brain-layers" aria-label="知识分层">
            <LayerCard
              kind="summaries"
              count={stats.summaries}
              title="摘要"
              hint="源材料的蒸馏"
            />
            <LayerCard
              kind="entities"
              count={stats.entities}
              title="实体"
              hint="人、物、符号、系统"
            />
            <LayerCard
              kind="concepts"
              count={stats.concepts}
              title="概念"
              hint="可复用的理解单元"
            />
            <LayerCard
              kind="synthesis"
              count={stats.synthesis}
              title="综合"
              hint="问答与跨页洞察"
            />
          </section>

          {stats.topics && stats.topics.length > 0 && (
            <section className="brain-constellation" aria-label="知识星图">
              <div className="section-head">
                <h2>知识星图</h2>
                <p>当前心智里活跃的概念与实体——点状的覆盖域。</p>
              </div>
              <div className="topic-cloud">
                {stats.topics.map((t, i) => (
                  <span
                    key={`${t.type}-${t.title}-${i}`}
                    className={`topic-chip type-${t.type}`}
                    style={{ animationDelay: `${(i % 12) * 0.08}s` }}
                    title={typeLabel(t.type)}
                  >
                    {t.title}
                  </span>
                ))}
              </div>
            </section>
          )}

          {stats.recent && stats.recent.length > 0 && (
            <section className="brain-recent" aria-label="近期编织">
              <div className="section-head">
                <h2>近期编织</h2>
                <p>知识库最近在长出什么。</p>
              </div>
              <ul className="recent-list">
                {stats.recent.map((r, i) => (
                  <li key={`${r.date}-${r.title}-${i}`}>
                    <span className="recent-date">{r.date || '—'}</span>
                    <span className={`recent-op op-${r.operation}`}>
                      {opLabel(r.operation)}
                    </span>
                    <span className="recent-type">{typeLabel(r.page_type)}</span>
                    <span className="recent-title">{r.title}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {stats.pages === 0 && (
            <section className="brain-empty">
              <p>知识库还是一片安静的空白。</p>
              <p className="muted">
                用 CLI <code>ruminate ingest …</code> 喂入第一份材料，这里会开始生长。
              </p>
            </section>
          )}

          <section className="brain-cta-row">
            <NavLink to="/chat" className="cta-primary">
              向知识库提问
              <span className="cta-arrow">→</span>
            </NavLink>
            <p className="cta-sub">
              它已经读过你的材料；现在轮到你追问、对照、验证。
            </p>
          </section>
        </>
      )}
    </div>
  )
}

function LayerCard({
  kind,
  count,
  title,
  hint,
}: {
  kind: string
  count: number
  title: string
  hint: string
}) {
  return (
    <article className={`layer-card layer-${kind}`}>
      <div className="layer-count">{count}</div>
      <div className="layer-title">{title}</div>
      <div className="layer-hint">{hint}</div>
    </article>
  )
}
