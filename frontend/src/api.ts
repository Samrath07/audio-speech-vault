export type Role = 'superadmin' | 'admin' | 'researcher' | 'reviewer'

export type User = {
  id: string
  email: string
  displayName: string
  role: Role
  isActive: boolean
  mustChangePassword: boolean
}

export type Session = {
  user: User
  csrfToken: string
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export async function api<T>(path: string, options: RequestInit = {}, csrfToken?: string): Promise<T> {
  const response = await fetch(path, {
    ...options,
    credentials: 'same-origin',
    cache: 'no-store',
    headers: {
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...(options.method && options.method !== 'GET' ? { 'X-Requested-With': 'AudioSpeechVault' } : {}),
      ...(csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
      ...options.headers,
    },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string }
    throw new ApiError(response.status, body.error || 'Request failed')
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>
}
