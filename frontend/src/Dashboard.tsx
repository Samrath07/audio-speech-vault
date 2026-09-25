import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Activity, ArrowRight, AudioLines, CircleCheck, CircleX, ClipboardCheck, Database, FileAudio, FolderOpen, LayoutDashboard, LoaderCircle, LogOut, RefreshCw, Shield, UserRound } from 'lucide-react'
import { api, type Session } from './api'
import Users from './Users'
import AccessRequests from './AccessRequests'
import Projects from './Projects'
import './summary.css'

type CheckState = 'checking' | 'healthy' | 'unavailable'
type View = 'overview' | 'projects' | 'requests' | 'users' | 'account'

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

type ProjectSummary = { id: string; code: string; name: string }
type RecordingSummary = { id: string; projectId: string; filename: string; durationMs: number; taskStatus: string }

function Overview({ session, onOpenProjects }: { session: Session; onOpenProjects: () => void }) {
  const [apiState, setApiState] = useState<CheckState>('checking')
  const [databaseState, setDatabaseState] = useState<CheckState>('checking')
  const [checkedAt, setCheckedAt] = useState<Date | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [projects, setProjects] = useState<ProjectSummary[]>([])
  const [recordings, setRecordings] = useState<RecordingSummary[]>([])

  const refresh = useCallback(async () => {
    setRefreshing(true)
    const [apiStateResult, database, accessibleProjects] = await Promise.all([checkEndpoint('/health/live'), checkEndpoint('/health/ready'), api<ProjectSummary[]>('/api/projects').catch(() => [])])
    setApiState(apiStateResult)
    setDatabaseState(database)
    setProjects(accessibleProjects)
    const projectRecordings = await Promise.all(accessibleProjects.map(project => api<RecordingSummary[]>(`/api/projects/${project.id}/recordings`).catch(() => [])))
    setRecordings(projectRecordings.flat())
    setCheckedAt(new Date())
    setRefreshing(false)
  }, [])

  useEffect(() => {
    void refresh()
    const interval = window.setInterval(() => void refresh(), 30_000)
    return () => window.clearInterval(interval)
  }, [refresh])

  const actionStatuses = session.user.role === 'reviewer' ? ['submitted'] : session.user.role === 'researcher' ? ['assigned', 'in_progress', 'changes_requested'] : ['unassigned', 'submitted', 'changes_requested']
  const attention = recordings.filter(recording => actionStatuses.includes(recording.taskStatus))
  const complete = recordings.filter(recording => recording.taskStatus === 'approved').length
  const greeting = session.user.role === 'reviewer' ? 'Recordings ready for review' : session.user.role === 'researcher' ? 'Continue your annotation work' : 'Workspace activity and delivery status'
  return <>
    <div className="page-heading dashboard-heading"><div><p className="eyebrow">Audio Speech Vault</p><h1>Good to see you, {session.user.displayName.split(' ')[0]}</h1><p className="page-subtitle">{greeting}</p></div><button className="refresh-button" type="button" onClick={() => void refresh()} disabled={refreshing} title="Refresh dashboard" aria-label="Refresh dashboard"><RefreshCw className={refreshing ? 'spin' : ''} size={18} /></button></div>
    <section className="metric-strip" aria-label="Workspace summary"><div><span>Accessible projects</span><strong>{projects.length}</strong><FolderOpen size={18} /></div><div><span>Total recordings</span><strong>{recordings.length}</strong><FileAudio size={18} /></div><div><span>Needs attention</span><strong>{attention.length}</strong><ClipboardCheck size={18} /></div><div><span>Approved</span><strong>{complete}</strong><CircleCheck size={18} /></div></section>
    <section className="section work-queue" aria-labelledby="work-heading"><div className="section-heading"><div><h2 id="work-heading">Your work queue</h2><p>Recordings that need the next action</p></div><button className="text-button" type="button" onClick={onOpenProjects}>View projects <ArrowRight size={15} /></button></div>{attention.length ? <div className="queue-list">{attention.slice(0, 6).map(recording => { const project = projects.find(item => item.id === recording.projectId); return <button key={recording.id} onClick={onOpenProjects}><span className="queue-file"><FileAudio size={17} /><span><strong>{recording.filename}</strong><small>{project?.name || 'Project'} · {(recording.durationMs / 1000).toFixed(1)} seconds</small></span></span><span className={`task-state task-state--${recording.taskStatus}`}>{recording.taskStatus.replace('_', ' ')}</span><ArrowRight size={16} /></button> })}</div> : <div className="queue-empty"><CircleCheck size={24} /><div><strong>You are all caught up</strong><span>No recordings currently require your attention.</span></div></div>}</section>
    <section className="section project-overview" aria-labelledby="projects-heading"><div className="section-heading"><div><h2 id="projects-heading">Projects</h2><p>Your accessible research workspaces</p></div><span>{projects.length} accessible</span></div>{projects.length ? <div className="project-summary-list">{projects.map(project => <button key={project.id} onClick={onOpenProjects}><FolderOpen size={17} /><strong>{project.name}</strong><span>{project.code}</span><ArrowRight size={15} /></button>)}</div> : <div className="empty-state"><div className="empty-icon"><FolderOpen size={24} /></div><h3>No accessible projects</h3></div>}</section>
    <section className="system-strip" aria-labelledby="system-heading"><div><h2 id="system-heading">System status</h2><span>{checkedAt ? `Checked ${checkedAt.toLocaleTimeString()}` : 'Checking services'}</span></div><div><Activity size={16} /><span>Application</span><StatusLabel state={apiState} /></div><div><Database size={16} /><span>Database</span><StatusLabel state={databaseState} /></div></section>
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

  return <div className="app-shell"><aside className="sidebar" aria-label="Primary navigation"><div className="brand"><span className="brand-mark"><AudioLines size={25} strokeWidth={2.2} /></span><span>Audio Speech<br />Vault</span></div><nav className="nav-list" aria-label="Workspace"><button className={`nav-item ${view === 'overview' ? 'nav-item--active' : ''}`} onClick={() => setView('overview')} title="Overview" aria-current={view === 'overview' ? 'page' : undefined}><LayoutDashboard size={19} />Overview</button><button className={`nav-item ${view === 'projects' ? 'nav-item--active' : ''}`} onClick={() => setView('projects')} title="Projects" aria-current={view === 'projects' ? 'page' : undefined}><FolderOpen size={19} />Projects</button>{session.user.role === 'superadmin' && <button className={`nav-item ${view === 'requests' ? 'nav-item--active' : ''}`} onClick={() => setView('requests')} title="Access requests" aria-current={view === 'requests' ? 'page' : undefined}><ClipboardCheck size={19} />Access requests</button>}{session.user.role === 'superadmin' && <button className={`nav-item ${view === 'users' ? 'nav-item--active' : ''}`} onClick={() => setView('users')} title="Users" aria-current={view === 'users' ? 'page' : undefined}><Shield size={19} />Users</button>}<button className={`nav-item ${view === 'account' ? 'nav-item--active' : ''}`} onClick={() => setView('account')} title="Account" aria-current={view === 'account' ? 'page' : undefined}><UserRound size={19} />Account</button></nav><div className="sidebar-footer"><span className="sidebar-footer-dot" />Development workspace</div></aside><main className="main-content"><header className="topbar"><span>Workspace</span><div className="topbar-actions"><span className="topbar-user">{session.user.displayName} <span className="topbar-role">{session.user.role}</span></span><button className="topbar-logout" type="button" onClick={() => void logout()} title="Sign out" aria-label="Sign out"><LogOut size={18} /></button></div></header><div className={`content-inner ${view === 'projects' ? 'content-inner--workspace' : ''}`}>{error && <p className="form-error" role="alert">{error}</p>}{view === 'overview' && <Overview session={session} onOpenProjects={() => setView('projects')} />}{view === 'projects' && <Projects session={session} />}{view === 'requests' && session.user.role === 'superadmin' && <AccessRequests session={session} />}{view === 'users' && session.user.role === 'superadmin' && <Users session={session} onSessionInvalid={onLogout} />}{view === 'account' && <Account session={session} onLogout={onLogout} />}</div></main></div>
}
