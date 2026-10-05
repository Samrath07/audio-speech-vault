import { useState, type FormEvent } from 'react'
import { AudioLines, ArrowRight, LockKeyhole } from 'lucide-react'
import { api, type Session } from './api'

export default function Login({ onLogin, serverError, onRequestAccess, onForgotPassword }: { onLogin: (session: Session) => void; serverError: string; onRequestAccess: () => void; onForgotPassword: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState(serverError)
  const [submitting, setSubmitting] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      const session = await api<Session>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      })
      onLogin(session)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Sign in failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="login-page">
      <div className="login-brand"><span className="brand-mark"><AudioLines size={27} /></span><span>Audio Speech Vault</span></div>
      <form className="login-form" onSubmit={submit}>
        <div className="login-icon"><LockKeyhole size={22} /></div>
        <h1>Sign in</h1>
        <p>Access your research workspace.</p>
        <label htmlFor="email">Email address</label>
        <input id="email" type="email" autoComplete="username" value={email} onChange={event => setEmail(event.target.value)} required />
        <div className="password-label"><label htmlFor="password">Password</label><button className="text-button" type="button" onClick={onForgotPassword}>Forgot password?</button></div>
        <input id="password" type="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} required />
        {error && <p className="form-error" role="alert">{error}</p>}
        <button className="primary-button login-submit" type="submit" disabled={submitting}>Sign in <ArrowRight size={17} /></button>
        <button className="text-button request-link" type="button" onClick={onRequestAccess}>Request institutional access</button>
      </form>
    </main>
  )
}
