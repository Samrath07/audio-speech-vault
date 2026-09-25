import { useState, type FormEvent } from 'react'
import { ArrowLeft, AudioLines, Building2, Send } from 'lucide-react'
import { api, type Role } from './api'

export default function RequestAccess({ onBack }: { onBack: () => void }) {
  const [institutionName, setInstitutionName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [email, setEmail] = useState('')
  const [requestedRole, setRequestedRole] = useState<Role>('researcher')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      const result = await api<{ message: string }>('/api/access-requests', { method: 'POST', body: JSON.stringify({ institutionName, displayName, email, requestedRole }) })
      setMessage(result.message)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to submit request')
    } finally {
      setBusy(false)
    }
  }

  return <main className="login-page"><div className="login-brand"><span className="brand-mark"><AudioLines size={27} /></span><span>Audio Speech Vault</span></div><form className="login-form access-form" onSubmit={submit}><button className="text-button back-button" type="button" onClick={onBack}><ArrowLeft size={16} /> Sign in</button><div className="login-icon"><Building2 size={22} /></div><h1>Request access</h1><p>Use your institution-issued email address.</p>{message ? <div className="success-message" role="status"><strong>Request received</strong><span>{message}</span></div> : <><label htmlFor="institution">Institution name</label><input id="institution" value={institutionName} onChange={event => setInstitutionName(event.target.value)} maxLength={160} required /><label htmlFor="request-name">Full name</label><input id="request-name" autoComplete="name" value={displayName} onChange={event => setDisplayName(event.target.value)} maxLength={100} required /><label htmlFor="request-email">Institutional email</label><input id="request-email" type="email" autoComplete="email" value={email} onChange={event => setEmail(event.target.value)} required /><label htmlFor="request-role">Requested role</label><select id="request-role" value={requestedRole} onChange={event => setRequestedRole(event.target.value as Role)}><option value="researcher">Researcher</option><option value="reviewer">Reviewer</option><option value="admin">Administrator</option></select>{error && <p className="form-error" role="alert">{error}</p>}<button className="primary-button login-submit" type="submit" disabled={busy}>Submit request <Send size={16} /></button></>}</form></main>
}
