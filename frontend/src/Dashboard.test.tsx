import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import Dashboard from './Dashboard'
import type { Role, Session } from './api'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (input: string) => ({
    ok: true,
    json: async () => input === '/api/users' ? { users: [] } : { status: 'ok' },
  })))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function sessionFor(role: Role): Session {
  return {
    user: { id: 'user-id', email: `${role}@example.test`, displayName: 'Test User', role, isActive: true, mustChangePassword: false },
    csrfToken: 'test-csrf-token',
  }
}

describe('role dashboard', () => {
  for (const role of ['superadmin', 'admin', 'researcher', 'reviewer'] as Role[]) {
    it(`shows the overview and account to ${role}`, () => {
      render(<Dashboard session={sessionFor(role)} onLogout={() => {}} />)
      expect(screen.getByRole('heading', { name: 'Overview' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Account' })).toBeInTheDocument()
      expect(screen.getByText(role)).toBeInTheDocument()
      if (role === 'superadmin') {
        expect(screen.getByRole('button', { name: 'Users' })).toBeInTheDocument()
        expect(screen.getByRole('button', { name: 'Access requests' })).toBeInTheDocument()
      } else {
        expect(screen.queryByRole('button', { name: 'Users' })).not.toBeInTheDocument()
        expect(screen.queryByRole('button', { name: 'Access requests' })).not.toBeInTheDocument()
      }
    })
  }

  it('opens account management for superadmin', async () => {
    render(<Dashboard session={sessionFor('superadmin')} onLogout={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: 'Users' }))
    expect(await screen.findByRole('heading', { name: 'Users' })).toBeInTheDocument()
  })
})
