import { useCallback, useEffect, useState } from 'react'
import { Check, RefreshCw, X } from 'lucide-react'
import { api, type Role, type Session } from './api'

type AccessRequest = {
  id: string
  institutionName: string
  email: string
  displayName: string
  requestedRole: Role
  status: 'email_pending' | 'pending' | 'approved' | 'rejected' | 'expired'
  rejectionReason?: string
  createdAt: string
}

export default function AccessRequests({ session }: { session: Session }) {
  const [requests, setRequests] = useState<AccessRequest[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [roles, setRoles] = useState<Record<string, Role>>({})
  const load = useCallback(async () => {
    try {
      const result = await api<{ requests: AccessRequest[] }>('/api/access-requests')
      setRequests(result.requests)
      setRoles(Object.fromEntries(result.requests.map(item => [item.id, item.requestedRole])))
      setError('')
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to load requests') }
  }, [])
  useEffect(() => { void load() }, [load])

  async function approve(request: AccessRequest) {
    setBusy(request.id)
    try {
      await api(`/api/access-requests/${request.id}/approve`, { method: 'POST', body: JSON.stringify({ role: roles[request.id] }) }, session.csrfToken)
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to approve request') }
    finally { setBusy('') }
  }

  async function reject(request: AccessRequest) {
    const reason = window.prompt(`Reason for rejecting ${request.displayName}'s request:`)
    if (!reason) return
    setBusy(request.id)
    try {
      await api(`/api/access-requests/${request.id}/reject`, { method: 'POST', body: JSON.stringify({ reason }) }, session.csrfToken)
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to reject request') }
    finally { setBusy('') }
  }

  async function resend(request: AccessRequest) {
    setBusy(request.id)
    try {
      await api(`/api/access-requests/${request.id}/resend-setup`, { method: 'POST' }, session.csrfToken)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to resend setup email') }
    finally { setBusy('') }
  }

  const pending = requests.filter(item => item.status === 'pending')
  const history = requests.filter(item => item.status !== 'pending')
  return <><div className="page-heading"><div><p className="eyebrow">Administration</p><h1>Access requests</h1><p className="page-subtitle">Review verified institutional requests</p></div><button className="refresh-button" type="button" onClick={() => void load()} title="Refresh requests" aria-label="Refresh requests"><RefreshCw size={18} /></button></div>{error && <p className="form-error" role="alert">{error}</p>}<section className="section"><div className="section-heading"><h2>Awaiting review</h2><span>{pending.length} pending</span></div>{pending.length === 0 ? <div className="empty-state compact-empty"><h3>No verified requests waiting</h3></div> : <div className="request-list">{pending.map(request => <article className="request-row" key={request.id}><div><h3>{request.displayName}</h3><p>{request.email}</p></div><div><strong>{request.institutionName}</strong><span>Requested {request.requestedRole}</span></div><select aria-label={`Approved role for ${request.displayName}`} value={roles[request.id]} onChange={event => setRoles(current => ({ ...current, [request.id]: event.target.value as Role }))}><option value="researcher">Researcher</option><option value="reviewer">Reviewer</option><option value="admin">Administrator</option></select><div className="request-actions"><button className="approve-button" disabled={busy === request.id} type="button" onClick={() => void approve(request)} title="Approve"><Check size={17} /></button><button className="reject-button" disabled={busy === request.id} type="button" onClick={() => void reject(request)} title="Reject"><X size={17} /></button></div></article>)}</div>}</section><section className="section"><div className="section-heading"><h2>Request history</h2></div><div className="table-wrap"><table className="users-table"><thead><tr><th>Applicant</th><th>Institution</th><th>Role</th><th>Status</th><th>Action</th></tr></thead><tbody>{history.map(request => <tr key={request.id}><td><strong>{request.displayName}</strong><span>{request.email}</span></td><td>{request.institutionName}</td><td>{request.requestedRole}</td><td><span className={`request-status request-status--${request.status}`}>{request.status.replace('_', ' ')}</span></td><td>{request.status === 'approved' && <button className="text-button" disabled={busy === request.id} type="button" onClick={() => void resend(request)}>Resend setup</button>}</td></tr>)}</tbody></table></div></section></>
}
