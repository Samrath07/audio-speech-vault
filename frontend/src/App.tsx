import { useCallback, useEffect, useState } from 'react'
import { Activity, AudioLines, CircleCheck, CircleX, Database, FolderOpen, LayoutDashboard, LoaderCircle, RefreshCw } from 'lucide-react'

type CheckState = 'checking' | 'healthy' | 'unavailable'

type ServiceStatus = {
  api: CheckState
  database: CheckState
  checkedAt: Date | null
}

const initialStatus: ServiceStatus = {
  api: 'checking',
  database: 'checking',
  checkedAt: null,
}

async function checkEndpoint(path: string): Promise<CheckState> {
  try {
    const response = await fetch(path, { cache: 'no-store' })
    return response.ok ? 'healthy' : 'unavailable'
  } catch {
    return 'unavailable'
  }
}

function StatusIcon({ state }: { state: CheckState }) {
  if (state === 'checking') return <LoaderCircle aria-hidden="true" className="spin" size={18} />
  if (state === 'healthy') return <CircleCheck aria-hidden="true" size={18} />
  return <CircleX aria-hidden="true" size={18} />
}

function StatusLabel({ state }: { state: CheckState }) {
  const label = state === 'checking' ? 'Checking' : state === 'healthy' ? 'Operational' : 'Unavailable'
  return <span className={`status status--${state}`}><StatusIcon state={state} />{label}</span>
}

export default function App() {
  const [status, setStatus] = useState<ServiceStatus>(initialStatus)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async () => {
    setRefreshing(true)
    const [api, database] = await Promise.all([
      checkEndpoint('/health/live'),
      checkEndpoint('/health/ready'),
    ])
    setStatus({ api, database, checkedAt: new Date() })
    setRefreshing(false)
  }, [])

  useEffect(() => {
    void refresh()
    const interval = window.setInterval(() => void refresh(), 30_000)
    return () => window.clearInterval(interval)
  }, [refresh])

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand"><span className="brand-mark"><AudioLines size={25} strokeWidth={2.2} /></span><span>Audio Speech<br />Vault</span></div>
        <nav className="nav-list" aria-label="Workspace">
          <a className="nav-item nav-item--active" href="/" aria-current="page"><LayoutDashboard size={19} />Overview</a>
          <span className="nav-item nav-item--disabled" aria-disabled="true"><FolderOpen size={19} />Projects</span>
          <span className="nav-item nav-item--disabled" aria-disabled="true"><AudioLines size={19} />Recordings</span>
        </nav>
        <div className="sidebar-footer"><span className="sidebar-footer-dot" />Development workspace</div>
      </aside>

      <main className="main-content">
        <header className="topbar"><span>Workspace</span><span className="topbar-env">Development</span></header>
        <div className="content-inner">
          <div className="page-heading">
            <div><p className="eyebrow">Audio Speech Vault</p><h1>Overview</h1><p className="page-subtitle">Service and database status</p></div>
            <button className="refresh-button" type="button" onClick={() => void refresh()} disabled={refreshing} title="Refresh status" aria-label="Refresh status"><RefreshCw className={refreshing ? 'spin' : ''} size={18} /></button>
          </div>

          <section className="section" aria-labelledby="system-heading">
            <div className="section-heading"><h2 id="system-heading">System status</h2><span>{status.checkedAt ? `Checked ${status.checkedAt.toLocaleTimeString()}` : 'Checking services'}</span></div>
            <div className="status-grid">
              <div className="status-card"><div className="status-card-icon"><Activity size={21} /></div><div><h3>Application server</h3><p>HTTP service</p></div><StatusLabel state={status.api} /></div>
              <div className="status-card"><div className="status-card-icon"><Database size={21} /></div><div><h3>PostgreSQL</h3><p>Database connection</p></div><StatusLabel state={status.database} /></div>
            </div>
          </section>

          <section className="section" aria-labelledby="projects-heading">
            <div className="section-heading"><h2 id="projects-heading">Projects</h2></div>
            <div className="empty-state"><div className="empty-icon"><FolderOpen size={24} /></div><h3>No projects yet</h3></div>
          </section>
        </div>
      </main>
    </div>
  )
}
