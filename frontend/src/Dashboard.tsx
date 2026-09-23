import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Activity, AudioLines, CircleCheck, CircleX, ClipboardCheck, Database, FolderOpen, LayoutDashboard, LoaderCircle, LogOut, RefreshCw, Shield, UserRound } from 'lucide-react'
import { api, type Session } from './api'
import Users from './Users'
import AccessRequests from './AccessRequests'

type CheckState = 'checking' | 'healthy' | 'unavailable'
type View = 'overview' | 'requests' | 'users' | 'account'

function StatusLabel({ state }: { state: CheckState }) {
  const label = state === 'checking' ? 'Checking' : state === 'healthy' ? 'Operational' : 'Unavailable'
  const Icon = state === 'checking' ? LoaderCircle : state === 'healthy' ? CircleCheck : CircleX
  return <span className={`status status--${state}`}><Icon className={state === 'checking' ? 'spin' : ''} size={18} />{label}</span>
}

async function checkEndpoint(path: string): Promise<CheckState> {
  try {
    const response = await fetch(path, { cache: 'no-store' })
    return response.ok ? 'healthy' : 'unavailable'
  } catch {
    return 'unavailable'
  }
}

function Overview() {
  const [apiState, setApiState] = useState<CheckState>('checking')
  const [databaseState, setDatabaseState] = useState<CheckState>('checking')
  const [checkedAt, setCheckedAt] = useState<Date | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = useCallback(async () => {
    setRefreshing(true)
    const [api, database] = await Promise.all([checkEndpoint('/health/live'), checkEndpoint('/health/ready')])
    setApiState(api)
    setDatabaseState(database)
    setCheckedAt(new Date())
    setRefreshing(false)
  }, [])

  useEffect(() => {
    void refresh()
    const interval = window.setInterval(() => void refresh(), 30_000)
    return () => window.clearInterval(interval)
  }, [refresh])

  return <>
    <div className="page-heading"><div><p className="eyebrow">Audio Speech Vault</p><h1>Overview</h1><p className="page-subtitle">Service and database status</p></div><button className="refresh-button" type="button" onClick={() => void refresh()} disabled={refreshing} title="Refresh status" aria-label="Refresh status"><RefreshCw className={refreshing ? 'spin' : ''} size={18} /></button></div>
    <section className="section" aria-labelledby="system-heading"><div className="section-heading"><h2 id="system-heading">System status</h2><span>{checkedAt ? `Checked ${checkedAt.toLocaleTimeString()}` : 'Checking services'}</span></div><div className="status-grid"><div className="status-card"><div className="status-card-icon"><Activity size={21} /></div><div><h3>Application server</h3><p>HTTP service</p></div><StatusLabel state={apiState} /></div><div className="status-card"><div className="status-card-icon"><Database size={21} /></div><div><h3>PostgreSQL</h3><p>Database connection</p></div><StatusLabel state={databaseState} /></div></div></section>
    <section className="section" aria-labelledby="projects-heading"><div className="section-heading"><h2 id="projects-heading">Projects</h2></div><div className="empty-state"><div className="empty-icon"><FolderOpen size={24} /></div><h3>No projects yet</h3></div></section>
  </>
}

function Account({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setMessage('')
    try {
      await api('/api/auth/change-password', { method: 'POST', body: JSON.stringify({ currentPassword, newPassword }) }, session.csrfToken)
      onLogout()
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'Password change failed')
    } finally {
      setBusy(false)
    }
  }

  return <><div className="page-heading"><div><p className="eyebrow">Account</p><h1>Your account</h1><p className="page-subtitle">{session.user.email}</p></div></div><section className="section account-section"><div className="section-heading"><h2>Profile</h2></div><dl className="profile-list"><div><dt>Name</dt><dd>{session.user.displayName}</dd></div><div><dt>Role</dt><dd className="role-text">{session.user.role}</dd></div></dl></section><section className="section account-section"><div className="section-heading"><h2>Change password</h2></div><form className="stack-form" onSubmit={changePassword}><label htmlFor="current-password">Current password</label><input id="current-password" type="password" autoComplete="current-password" value={currentPassword} onChange={event => setCurrentPassword(event.target.value)} required /><label htmlFor="new-password">New password</label><input id="new-password" type="password" autoComplete="new-password" minLength={12} maxLength={72} value={newPassword} onChange={event => setNewPassword(event.target.value)} required /><p className="field-hint">12 to 72 bytes</p>{message && <p className="form-error" role="alert">{message}</p>}<button className="primary-button" type="submit" disabled={busy}>Update password</button></form></section></>
}

export default function Dashboard({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const [view, setView] = useState<View>('overview')
  const [error, setError] = useState('')

  async function logout() {
    setError('')
    try {
      await api('/api/auth/logout', { method: 'POST' }, session.csrfToken)
      onLogout()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Sign out failed')
    }
  }

  return <div className="app-shell"><aside className="sidebar" aria-label="Primary navigation"><div className="brand"><span className="brand-mark"><AudioLines size={25} strokeWidth={2.2} /></span><span>Audio Speech<br />Vault</span></div><nav className="nav-list" aria-label="Workspace"><button className={`nav-item ${view === 'overview' ? 'nav-item--active' : ''}`} onClick={() => setView('overview')} title="Overview" aria-current={view === 'overview' ? 'page' : undefined}><LayoutDashboard size={19} />Overview</button>{session.user.role === 'superadmin' && <button className={`nav-item ${view === 'requests' ? 'nav-item--active' : ''}`} onClick={() => setView('requests')} title="Access requests" aria-current={view === 'requests' ? 'page' : undefined}><ClipboardCheck size={19} />Access requests</button>}{session.user.role === 'superadmin' && <button className={`nav-item ${view === 'users' ? 'nav-item--active' : ''}`} onClick={() => setView('users')} title="Users" aria-current={view === 'users' ? 'page' : undefined}><Shield size={19} />Users</button>}<button className={`nav-item ${view === 'account' ? 'nav-item--active' : ''}`} onClick={() => setView('account')} title="Account" aria-current={view === 'account' ? 'page' : undefined}><UserRound size={19} />Account</button></nav><div className="sidebar-footer"><span className="sidebar-footer-dot" />Development workspace</div></aside><main className="main-content"><header className="topbar"><span>Workspace</span><div className="topbar-actions"><span className="topbar-user">{session.user.displayName} <span className="topbar-role">{session.user.role}</span></span><button className="topbar-logout" type="button" onClick={() => void logout()} title="Sign out" aria-label="Sign out"><LogOut size={18} /></button></div></header><div className="content-inner">{error && <p className="form-error" role="alert">{error}</p>}{view === 'overview' && <Overview />}{view === 'requests' && session.user.role === 'superadmin' && <AccessRequests session={session} />}{view === 'users' && session.user.role === 'superadmin' && <Users session={session} onSessionInvalid={onLogout} />}{view === 'account' && <Account session={session} onLogout={onLogout} />}</div></main></div>
}
