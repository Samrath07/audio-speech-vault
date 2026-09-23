import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Plus, RefreshCw, X } from 'lucide-react'
import { api, type Role, type Session, type User } from './api'

const roles: Role[] = ['superadmin', 'admin', 'researcher', 'reviewer']

export default function Users({ session, onSessionInvalid }: { session: Session; onSessionInvalid: () => void }) {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('researcher')
  const [resetUser, setResetUser] = useState<User | null>(null)
  const [resetPassword, setResetPassword] = useState('')

  const load = useCallback(async () => {
    try {
      const result = await api<{ users: User[] }>('/api/users')
      setUsers(result.users)
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load users')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function createUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusyId('create')
    setError('')
    try {
      await api<User>('/api/users', { method: 'POST', body: JSON.stringify({ email, displayName, password, role }) }, session.csrfToken)
      setShowCreate(false)
      setEmail('')
      setDisplayName('')
      setPassword('')
      setRole('researcher')
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to create user')
    } finally {
      setBusyId('')
    }
  }

  async function updateUser(user: User, update: { role?: Role; isActive?: boolean }) {
    setBusyId(user.id)
    setError('')
    try {
      await api<User>(`/api/users/${user.id}`, { method: 'PATCH', body: JSON.stringify(update) }, session.csrfToken)
      if (user.id === session.user.id) onSessionInvalid()
      else await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to update user')
    } finally {
      setBusyId('')
    }
  }

  async function resetUserPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!resetUser) return
    setBusyId(resetUser.id)
    setError('')
    try {
      await api(`/api/users/${resetUser.id}/password`, { method: 'POST', body: JSON.stringify({ password: resetPassword }) }, session.csrfToken)
      if (resetUser.id === session.user.id) onSessionInvalid()
      setResetUser(null)
      setResetPassword('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to reset password')
    } finally {
      setBusyId('')
    }
  }

  return <>
    <div className="page-heading"><div><p className="eyebrow">Administration</p><h1>Users</h1><p className="page-subtitle">Manage accounts and system roles</p></div><div className="heading-actions"><button className="refresh-button" type="button" onClick={() => void load()} title="Refresh users" aria-label="Refresh users"><RefreshCw size={18} /></button><button className="primary-button" type="button" onClick={() => setShowCreate(!showCreate)}><Plus size={17} /> Add user</button></div></div>
    {error && <p className="form-error" role="alert">{error}</p>}
    {showCreate && <section className="user-form-panel"><div className="section-heading"><h2>Create account</h2><button className="icon-button" type="button" onClick={() => setShowCreate(false)} title="Close" aria-label="Close"><X size={18} /></button></div><form className="user-create-form" onSubmit={createUser}><label>Full name<input autoComplete="name" value={displayName} onChange={event => setDisplayName(event.target.value)} maxLength={100} required /></label><label>Email address<input type="email" autoComplete="off" value={email} onChange={event => setEmail(event.target.value)} required /></label><label>Role<select value={role} onChange={event => setRole(event.target.value as Role)}>{roles.map(item => <option key={item} value={item}>{item}</option>)}</select></label><label>Initial password<input type="password" autoComplete="new-password" value={password} onChange={event => setPassword(event.target.value)} minLength={12} maxLength={72} required /></label><button className="primary-button" type="submit" disabled={busyId === 'create'}>Create account</button></form></section>}
    <section className="section" aria-label="User accounts">{loading ? <p className="muted">Loading accounts...</p> : <div className="table-wrap"><table className="users-table"><thead><tr><th>Name</th><th>Role</th><th>Status</th><th>Actions</th></tr></thead><tbody>{users.map(user => <tr key={user.id}><td><strong>{user.displayName}</strong><span>{user.email}</span></td><td><select aria-label={`Role for ${user.displayName}`} value={user.role} disabled={busyId === user.id} onChange={event => void updateUser(user, { role: event.target.value as Role })}>{roles.map(item => <option key={item} value={item}>{item}</option>)}</select></td><td><span className={`account-status ${user.isActive ? 'account-status--active' : ''}`}>{user.isActive ? 'Active' : 'Inactive'}</span></td><td><div className="row-actions"><button type="button" disabled={busyId === user.id} onClick={() => void updateUser(user, { isActive: !user.isActive })}>{user.isActive ? 'Deactivate' : 'Activate'}</button><button type="button" onClick={() => { setResetUser(user); setResetPassword('') }}>Reset password</button></div></td></tr>)}</tbody></table></div>}</section>
    {resetUser && <div className="modal-backdrop" role="presentation"><div className="modal" role="dialog" aria-modal="true" aria-labelledby="reset-title"><div className="section-heading"><h2 id="reset-title">Reset password</h2><button className="icon-button" type="button" onClick={() => setResetUser(null)} title="Close" aria-label="Close"><X size={18} /></button></div><p className="muted">{resetUser.displayName}</p><form className="stack-form" onSubmit={resetUserPassword}><label htmlFor="reset-password">New password</label><input id="reset-password" type="password" autoComplete="new-password" minLength={12} maxLength={72} value={resetPassword} onChange={event => setResetPassword(event.target.value)} required /><button className="primary-button" type="submit" disabled={busyId === resetUser.id}>Reset password</button></form></div></div>}
  </>
}
