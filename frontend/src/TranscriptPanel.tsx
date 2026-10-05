import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { BookOpen, Check, CircleAlert, Download, Eye, MessageSquareText, Plus, Trash2, X } from 'lucide-react'
import { api, type Session } from './api'
import './transcript.css'

type SpeakerRole = 'target' | 'interviewer' | 'other' | 'unknown' | 'overlap'
type Segment = { id: string; recordingId: string; startMs: number; endMs: number; speakerRole: SpeakerRole; text: string; source: 'human' | 'machine'; version: number }
type Finding = { code: string; severity: 'error' | 'warning'; message: string }
type Transcript = { id: string; recordingId: string; conventionVersion: number; targetSpeakerSource?: 'speaker_a' | 'speaker_b' | 'manual'; segments: Segment[]; findings: Finding[]; canEdit: boolean }
type Convention = { token: string; label: string; description: string; insertion?: string }

const conventions: { name: string; items: Convention[] }[] = [
  { name: 'Timing', items: [
    { token: '(.)', label: 'Micropause', description: 'Brief pause below the project timing threshold.' },
    { token: '(0.0)', label: 'Timed pause', description: 'Measured silence in seconds; replace 0.0 with the observed duration.' },
    { token: '=', label: 'Latched turns', description: 'No audible gap between adjacent turns.' },
  ] },
  { name: 'Delivery', items: [
    { token: 'wor-', label: 'Cut-off', description: 'Abruptly stopped word or sound.', insertion: '-' },
    { token: 'wo:rd', label: 'Sound stretch', description: 'A colon marks audible elongation; more colons indicate a longer stretch.', insertion: ':' },
    { token: 'CAPS', label: 'Louder', description: 'Speech noticeably louder than the surrounding talk.' },
    { token: '°quiet°', label: 'Quieter', description: 'Speech noticeably quieter than the surrounding talk.', insertion: '°°' },
    { token: '↑word', label: 'Pitch up', description: 'Marked upward pitch shift.', insertion: '↑' },
    { token: '↓word', label: 'Pitch down', description: 'Marked downward pitch shift.', insertion: '↓' },
    { token: '>faster<', label: 'Faster', description: 'Talk delivered faster than surrounding speech.', insertion: '><' },
    { token: '<slower>', label: 'Slower', description: 'Talk delivered slower than surrounding speech.', insertion: '<>' },
  ] },
  { name: 'Voice and events', items: [
    { token: '.hhh', label: 'Inbreath', description: 'Audible inhalation.' },
    { token: 'hhh', label: 'Outbreath', description: 'Audible exhalation.' },
    { token: '((sound))', label: 'Sound or action', description: 'Researcher description rather than spoken words.', insertion: '((sound/action))' },
    { token: '[overlap]', label: 'Overlap', description: 'Bracket the simultaneous portion of each speaker turn.', insertion: '[]' },
  ] },
  { name: 'Uncertainty', items: [
    { token: '(word)', label: 'Uncertain hearing', description: 'Best candidate hearing, not fully certain.', insertion: '(possible word)' },
    { token: '[?]', label: 'Unresolved', description: 'A brief ambiguous portion with no defensible candidate.' },
    { token: '[unintelligible]', label: 'Unintelligible', description: 'Speech is audible but cannot be transcribed.' },
  ] },
]

const speakerCode: Record<SpeakerRole, string> = { target: 'TGT', interviewer: 'INT', other: 'OTH', unknown: 'UNK', overlap: 'OVL' }
const time = (milliseconds: number) => `${Math.floor(milliseconds / 60000)}:${Math.floor(milliseconds % 60000 / 1000).toString().padStart(2, '0')}.${Math.floor(milliseconds % 1000).toString().padStart(3, '0')}`

function transcriptText(segments: Segment[]) {
  const title = 'Audio Jeffersonian transcript - approval draft\n'
  return title + segments.map((segment, index) => `${String(index + 1).padStart(3, '0')} ${speakerCode[segment.speakerRole]}:  ${segment.text}`).join('\n') + '\n'
}

function notationWarnings(text: string, role: SpeakerRole) {
  const warnings: string[] = []
  if ((text.match(/\[/g) || []).length !== (text.match(/\]/g) || []).length) warnings.push('Overlap or uncertainty brackets are not balanced.')
  if (text.includes('(0.0)')) warnings.push('Replace the timed-pause placeholder with the measured duration.')
  if (role === 'overlap' && !text.includes('[')) warnings.push('An overlapping turn should mark the simultaneous portion with brackets.')
  return warnings
}

export default function TranscriptPanel({ recordingId, session, startMs, endMs, durationMs, onJump }: { recordingId: string; session: Session; startMs: number; endMs: number; durationMs: number; onJump: (startMs: number, endMs: number) => void }) {
  const [transcript, setTranscript] = useState<Transcript | null>(null)
  const [editing, setEditing] = useState<Segment | null>(null)
  const [speakerRole, setSpeakerRole] = useState<SpeakerRole>('target')
  const [text, setText] = useState('')
  const [range, setRange] = useState({ startMs, endMs })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [showGuide, setShowGuide] = useState(false)
  const [showPreview, setShowPreview] = useState(false)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const warnings = useMemo(() => notationWarnings(text, speakerRole), [text, speakerRole])
  const refresh = () => api<Transcript>(`/api/recordings/${recordingId}/transcript`).then(setTranscript).catch((cause: Error) => setError(cause.message))
  useEffect(() => { void refresh() }, [recordingId])
  useEffect(() => { if (!editing) setRange({ startMs, endMs }) }, [startMs, endMs, editing])

  async function setTarget(source: string) {
    if (!source) return
    try { setTranscript(await api<Transcript>(`/api/recordings/${recordingId}/transcript/target-speaker`, { method: 'PUT', body: JSON.stringify({ source }) }, session.csrfToken)); setError('') } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to confirm target speaker') }
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    if (!text.trim() || warnings.length) return
    setBusy(true); setError('')
    try {
      await api(`/api/recordings/${recordingId}/transcript/segments${editing ? `/${editing.id}` : ''}`, { method: editing ? 'PATCH' : 'POST', body: JSON.stringify({ startMs: range.startMs, endMs: range.endMs, speakerRole, text, version: editing?.version }) }, session.csrfToken)
      setEditing(null); setText(''); setSpeakerRole('target'); await refresh()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to save transcript segment') } finally { setBusy(false) }
  }

  function insert(value: string) {
    const field = textarea.current
    const start = field?.selectionStart ?? text.length
    const end = field?.selectionEnd ?? text.length
    const leadingSpace = start > 0 && text[start - 1] !== ' ' && !['-', ':', '↑', '↓'].includes(value) ? ' ' : ''
    setText(`${text.slice(0, start)}${leadingSpace}${value}${text.slice(end)}`)
    requestAnimationFrame(() => { const position = start + leadingSpace.length + value.length; field?.focus(); field?.setSelectionRange(position, position) })
  }

  function edit(segment: Segment) {
    if (!transcript?.canEdit) return
    setEditing(segment); setSpeakerRole(segment.speakerRole); setText(segment.text); setRange({ startMs: segment.startMs, endMs: segment.endMs }); onJump(segment.startMs, segment.endMs)
  }

  async function remove(segment: Segment) {
    try { await api(`/api/recordings/${recordingId}/transcript/segments/${segment.id}`, { method: 'DELETE', body: JSON.stringify({ version: segment.version }) }, session.csrfToken); if (editing?.id === segment.id) { setEditing(null); setText('') }; await refresh() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to delete transcript segment') }
  }

  function download() {
    const blob = new Blob([transcriptText(transcript?.segments || [])], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = `transcript-${recordingId}.txt`; link.click(); URL.revokeObjectURL(url)
  }

  const invalidRange = range.startMs < 0 || range.endMs <= range.startMs || range.endMs > durationMs
  return <section className="transcript-panel" aria-labelledby="transcript-heading">
    <div className="transcript-heading"><div><span>Verbatim workspace</span><h2 id="transcript-heading"><MessageSquareText size={18} />Audio Jeffersonian transcript</h2></div><div className="transcript-heading-actions"><button className="secondary-button" type="button" onClick={() => setShowGuide(value => !value)}><BookOpen size={15} />Conventions</button><button className="secondary-button" type="button" onClick={() => setShowPreview(value => !value)}><Eye size={15} />Preview</button><button className="icon-button" type="button" onClick={download} disabled={!transcript?.segments.length} title="Download transcript" aria-label="Download transcript"><Download size={16} /></button><label>Target identified by<select value={transcript?.targetSpeakerSource || ''} disabled={!transcript?.canEdit} onChange={event => void setTarget(event.target.value)}><option value="">Not confirmed</option><option value="speaker_a">Acoustic Speaker A</option><option value="speaker_b">Acoustic Speaker B</option><option value="manual">Manual listening</option></select></label></div></div>
    <div className="transcript-standard"><strong>Approval draft</strong><span>Audio Jeffersonian v1 preserves timing, overlap, delivery, fillers, repetitions, false starts, and incomplete words. Suggested signal events require human confirmation.</span></div>
    {showGuide && <section className="convention-guide" aria-label="Proposed transcription conventions">
      <div className="convention-guide-heading"><div><strong>Proposed Audio Jeffersonian v1</strong><span>This project profile must be approved by the research team before it becomes the production standard.</span></div><button className="icon-button" type="button" onClick={() => setShowGuide(false)} aria-label="Close conventions"><X size={16} /></button></div>
      <div className="convention-grid">{conventions.map(group => <section className="convention-group" key={group.name}><h3>{group.name}</h3><div className="convention-items">{group.items.map(item => <div className="convention-row" key={item.label}><code>{item.token}</code><span><strong>{item.label}</strong><small>{item.description}</small></span></div>)}</div></section>)}</div>
      <p className="convention-note"><CircleAlert size={15} />Visual actions such as gaze and gestures are outside the audio-only profile and must never be inferred from sound.</p>
    </section>}
    {(transcript?.findings.length || 0) > 0 && <div className="transcript-findings">{transcript?.findings.map(item => <span className={`transcript-finding transcript-finding--${item.severity}`} key={item.code}><CircleAlert size={14} />{item.message}</span>)}</div>}
    {showPreview && <section className="transcript-preview" aria-label="Publication transcript preview"><div><strong>Publication preview</strong><span>Monospaced line layout for researcher review</span></div><pre>{transcriptText(transcript?.segments || [])}</pre></section>}
    <div className="transcript-segments">{transcript?.segments.map((segment, index) => <article className={`transcript-segment transcript-segment--${segment.speakerRole}`} key={segment.id} onClick={() => { onJump(segment.startMs, segment.endMs); edit(segment) }}><span className="transcript-line">{String(index + 1).padStart(2, '0')}</span><button type="button" className="transcript-time" onClick={event => { event.stopPropagation(); onJump(segment.startMs, segment.endMs) }}>{time(segment.startMs)}<small>{time(segment.endMs)}</small></button><span className="speaker-role">{speakerCode[segment.speakerRole]}</span><p>{segment.text}</p>{transcript.canEdit && <button type="button" className="icon-button" title="Delete segment" aria-label="Delete transcript segment" onClick={event => { event.stopPropagation(); void remove(segment) }}><Trash2 size={14} /></button>}</article>)}{transcript?.segments.length === 0 && <div className="transcript-empty">Select a waveform interval and add the first verbatim turn.</div>}</div>
    {transcript?.canEdit && <form className="transcript-composer" onSubmit={save}><div className="transcript-fields"><label>Speaker<select value={speakerRole} onChange={event => setSpeakerRole(event.target.value as SpeakerRole)}><option value="target">Target speaker</option><option value="interviewer">Interviewer</option><option value="other">Other speaker</option><option value="unknown">Unknown speaker</option><option value="overlap">Overlapping turn</option></select></label><label>Start<input type="number" min="0" max={durationMs / 1000} step="0.001" value={range.startMs / 1000} onChange={event => setRange(current => ({ ...current, startMs: Math.round(Number(event.target.value) * 1000) }))} /></label><label>End<input type="number" min="0.001" max={durationMs / 1000} step="0.001" value={range.endMs / 1000} onChange={event => setRange(current => ({ ...current, endMs: Math.round(Number(event.target.value) * 1000) }))} /></label></div><label className="transcript-text">Verbatim text<textarea ref={textarea} value={text} maxLength={8000} placeholder="Type exactly what is heard, including fillers and repetitions" onChange={event => setText(event.target.value)} /></label><div className="marker-groups">{conventions.map(group => <div key={group.name}><span>{group.name}</span><div>{group.items.map(item => <button type="button" key={item.label} onClick={() => insert(item.insertion || item.token)} title={item.description}>{item.token}</button>)}</div></div>)}</div>{warnings.length > 0 && <div className="notation-warnings" role="alert">{warnings.map(warning => <span key={warning}><CircleAlert size={14} />{warning}</span>)}</div>}<div className="transcript-actions"><button className="primary-button" type="submit" disabled={busy || invalidRange || !text.trim() || warnings.length > 0}>{editing ? <Check size={15} /> : <Plus size={15} />}{editing ? 'Save turn' : 'Add turn'}</button>{editing && <button className="secondary-button" type="button" onClick={() => { setEditing(null); setText(''); setRange({ startMs, endMs }) }}>Cancel edit</button>}</div></form>}
    {error && <p className="form-error" role="alert">{error}</p>}
  </section>
}
