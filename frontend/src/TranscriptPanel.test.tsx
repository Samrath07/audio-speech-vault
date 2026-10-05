import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TranscriptPanel from './TranscriptPanel'
import type { Session } from './api'

const session: Session = {
  user: { id: 'user-1', email: 'researcher@example.org', displayName: 'Researcher', role: 'researcher', isActive: true, mustChangePassword: false },
  csrfToken: 'csrf-test',
}

describe('verbatim transcript panel', () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals() })

  it('renders guardrails and preserves explicit markers in the composer', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      id: 'transcript-1', recordingId: 'recording-1', conventionVersion: 1, segments: [], canEdit: true,
      findings: [
        { code: 'target_speaker_unconfirmed', severity: 'error', message: 'Confirm how the target speaker was identified.' },
        { code: 'transcript_empty', severity: 'error', message: 'Add at least one transcript segment.' },
      ],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    render(<TranscriptPanel recordingId="recording-1" session={session} startMs={1000} endMs={2400} durationMs={10000} onJump={() => undefined} />)

    expect(await screen.findByText('Audio Jeffersonian transcript')).toBeInTheDocument()
    expect(screen.getByText('Confirm how the target speaker was identified.')).toBeInTheDocument()
    expect(screen.getByText(/preserves timing, overlap, delivery/)).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Target speaker' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '[unintelligible]' }))
    expect(screen.getByPlaceholderText(/Type exactly what is heard/)).toHaveValue('[unintelligible]')
    expect(screen.getByRole('button', { name: 'Add turn' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Conventions' }))
    expect(screen.getByText('Proposed Audio Jeffersonian v1')).toBeInTheDocument()
    expect(screen.getByText(/must never be inferred from sound/)).toBeInTheDocument()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/recordings/recording-1/transcript', expect.objectContaining({ credentials: 'same-origin' })))
  })

  it('shows a numbered publication preview and blocks unfinished notation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      id: 'transcript-1', recordingId: 'recording-1', conventionVersion: 1, canEdit: true, findings: [],
      segments: [{ id: 'segment-1', recordingId: 'recording-1', startMs: 100, endMs: 900, speakerRole: 'target', text: 'I- I went (.) home.', source: 'human', version: 1 }],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    render(<TranscriptPanel recordingId="recording-1" session={session} startMs={1000} endMs={2400} durationMs={10000} onJump={() => undefined} />)
    expect(await screen.findByText('I- I went (.) home.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(screen.getByText(/001 TGT:\s+I- I went/)).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText(/Type exactly what is heard/), { target: { value: '[unfinished' } })
    expect(screen.getByText(/brackets are not balanced/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add turn' })).toBeDisabled()
  })
})
