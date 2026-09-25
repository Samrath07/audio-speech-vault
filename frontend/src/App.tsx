import { useEffect, useState } from 'react'
import { AudioLines, LoaderCircle } from 'lucide-react'
import { api, ApiError, type Session } from './api'
import Dashboard from './Dashboard'
import Login from './Login'
import RequestAccess from './RequestAccess'
import { PasswordSetup, VerifyEmail } from './TokenAction'

export default function App() {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [requestingAccess, setRequestingAccess] = useState(false)
  const [verificationToken, setVerificationToken] = useState(() => new URLSearchParams(window.location.search).get('verify'))
  const [setupToken, setSetupToken] = useState(() => new URLSearchParams(window.location.search).get('setup'))

  useEffect(() => {
    if (verificationToken || setupToken) window.history.replaceState({}, '', window.location.pathname)
  }, [verificationToken, setupToken])

  useEffect(() => {
    api<Session>('/api/auth/me')
      .then(setSession)
      .catch((cause: unknown) => {
        if (!(cause instanceof ApiError && cause.status === 401)) setError('Unable to reach the server.')
      })
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="auth-loading"><AudioLines size={32} /><LoaderCircle className="spin" size={22} /><span>Loading workspace</span></div>
  if (verificationToken) return <VerifyEmail token={verificationToken} onDone={() => setVerificationToken(null)} />
  if (setupToken) return <PasswordSetup token={setupToken} onDone={() => setSetupToken(null)} />
  if (!session && requestingAccess) return <RequestAccess onBack={() => setRequestingAccess(false)} />
  if (!session) return <Login onLogin={setSession} serverError={error} onRequestAccess={() => setRequestingAccess(true)} />
  return <Dashboard session={session} onLogout={() => setSession(null)} />
}
