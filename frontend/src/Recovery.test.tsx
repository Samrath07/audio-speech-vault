import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ForgotPassword, PasswordReset } from './TokenAction'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('password recovery', () => {
  it('requests a reset without revealing whether the account exists', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ message: 'If an active account exists for that address, a password reset link has been sent' }), { status: 202, headers: { 'Content-Type': 'application/json' } }))
    render(<ForgotPassword onBack={() => {}} />)
    fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'person@example.edu' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send reset link' }))
    expect(await screen.findByText('Check your inbox')).toBeInTheDocument()
    expect(fetch).toHaveBeenCalledWith('/api/auth/forgot-password', expect.objectContaining({ method: 'POST' }))
  })

  it('requires matching passwords before submitting a reset', async () => {
    const request = vi.spyOn(globalThis, 'fetch')
    render(<PasswordReset token="one-time-token" onDone={() => {}} />)
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'first-password-123' } })
    fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'different-password-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Reset password' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Passwords do not match')
    await waitFor(() => expect(request).not.toHaveBeenCalled())
  })

  it('shows request failures as errors and keeps the form available', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: 'Invalid request origin' }), { status: 403, headers: { 'Content-Type': 'application/json' } }))
    render(<ForgotPassword onBack={() => {}} />)
    fireEvent.change(screen.getByLabelText('Email address'), { target: { value: 'person@example.edu' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send reset link' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid request origin')
    expect(screen.getByRole('button', { name: 'Send reset link' })).toBeInTheDocument()
    expect(screen.queryByText('Check your inbox')).not.toBeInTheDocument()
  })
})
