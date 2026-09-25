import { useEffect, useRef, useState, type FormEvent } from 'react'
import { AudioLines, CircleCheck, KeyRound, LoaderCircle } from 'lucide-react'
import { api } from './api'

export function VerifyEmail({ token, onDone }: { token: string; onDone: () => void }) {
  const [status, setStatus] = useState('Verifying your institutional email...')
  const [success, setSuccess] = useState(false)
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    api<{ message: string }>('/api/access-requests/verify', { method: 'POST', body: JSON.stringify({ token }) })
      .then(result => { setStatus(result.message); setSuccess(true) })
      .catch(cause => setStatus(cause instanceof Error ? cause.message : 'Verification failed'))
  }, [token])
  return <main className="login-page"><div className="login-brand"><span className="brand-mark"><AudioLines size={27} /></span><span>Audio Speech Vault</span></div><section className="login-form token-result">{success ? <CircleCheck size={28} /> : <LoaderCircle className="spin" size={28} />}<h1>Email verification</h1><p>{status}</p><button className="primary-button" type="button" onClick={onDone}>Return to sign in</button></section></main>
}

export function PasswordSetup({ token, onDone }: { token: string; onDone: () => void }) {
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [message, setMessage] = useState('')
  const [success, setSuccess] = useState(false)
  const [busy, setBusy] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password !== confirmation) { setMessage('Passwords do not match'); return }
    setBusy(true)
    setMessage('')
    try {
      const result = await api<{ message: string }>('/api/auth/complete-password-setup', { method: 'POST', body: JSON.stringify({ token, password }) })
      setMessage(result.message)
      setSuccess(true)
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'Unable to set password')
    } finally { setBusy(false) }
  }
  return <main className="login-page"><div className="login-brand"><span className="brand-mark"><AudioLines size={27} /></span><span>Audio Speech Vault</span></div><form className="login-form" onSubmit={submit}><div className="login-icon"><KeyRound size={22} /></div><h1>Set your password</h1><p>Create the password you will use to sign in.</p>{success ? <div className="success-message"><strong>Password created</strong><span>{message}</span><button className="primary-button" type="button" onClick={onDone}>Continue to sign in</button></div> : <><label htmlFor="setup-password">New password</label><input id="setup-password" type="password" autoComplete="new-password" minLength={12} maxLength={72} value={password} onChange={event => setPassword(event.target.value)} required /><label htmlFor="setup-confirmation">Confirm password</label><input id="setup-confirmation" type="password" autoComplete="new-password" minLength={12} maxLength={72} value={confirmation} onChange={event => setConfirmation(event.target.value)} required />{message && <p className="form-error" role="alert">{message}</p>}<button className="primary-button login-submit" type="submit" disabled={busy}>Set password</button></>}</form></main>
}
