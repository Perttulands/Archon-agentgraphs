import { Suspense, lazy, useLayoutEffect, useRef, useState } from 'react'
import { useEscapeKey } from './useEscapeKey'
import { HumanChannelChoice } from '../humanChannel/HumanChannelChoice'
import { HUMAN_CHANNEL_TIMING, type HumanChannel } from '../humanChannel/humanChannel'
import '../styles/formations-start-mission.css'

// The Markdown renderer is its own chunk; the hint reads as its source until it loads.
const Markdown = lazy(() => import('../evidence/Markdown'))

export interface RunInputs {
  cwd: string
  brief: string
  beadId: string
  limits: { maxDispatch: number; maxAttempts: number; wallClockSeconds: number; redact: boolean }
}

/** The time limit counts agent work only: a run waiting at a human gate does not spend it (form-t5o). */
export const TIME_LIMIT_HINT = 'The time limit counts how long the agents work, from the start of the run. Time the run waits for you at a gate doesn\u2019t count.'

/** What the brief field asks for when the mission gives no input hint of its own. */
export const DEFAULT_BRIEF_HINT = 'The input this run works on: the request, sketch or task its first step receives. The board stays reusable; each run takes its own brief.'

const LIMIT_HINTS = {
  maxDispatch: 'Formation executions across this run, including judge steps.',
  maxAttempts: 'Maximum visits to each node.',
}

function readableDuration(value: string): string {
  const seconds = Number(value)
  if (!Number.isSafeInteger(seconds) || seconds < 1) return ''
  return [[Math.floor(seconds / 3600), 'hour'], [Math.floor(seconds % 3600 / 60), 'minute'], [seconds % 60, 'second']]
    .filter(([amount]) => amount !== 0)
    .map(([amount, unit]) => `${amount} ${unit}${amount === 1 ? '' : 's'}`).join(' ')
}

export function StartMissionDialog({ title, beadId = '', inputHint = '', humanChannel = 'notify', onStart, onClose }: {
  title: string; beadId?: string; inputHint?: string
  /** The mission's human channel; a different choice is saved on the mission before the run starts. */
  humanChannel?: HumanChannel
  onStart: (inputs: RunInputs, humanChannel: HumanChannel) => Promise<void>; onClose: () => void
}) {
  const [inputs, setInputs] = useState({ cwd: '', brief: '', beadId, limits: { maxDispatch: '20', maxAttempts: '3', wallClockSeconds: '1800', redact: false } })
  const dialogRef = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialogRef.current?.querySelector<HTMLInputElement>('#start-mission-cwd')?.focus()
    return () => { if (opener?.isConnected) opener.focus() }
  }, [])
  const [channel, setChannel] = useState<HumanChannel>(humanChannel)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useLayoutEffect(() => {
    if (!saving) return
    const dialog = dialogRef.current
    const focused = document.activeElement
    // Disabling the submit button can move browser focus to the document body.
    if (dialog && (!dialog.contains(focused) || focused?.matches(':disabled'))) {
      dialog.querySelector<HTMLInputElement>('#start-mission-cwd')?.focus()
    }
  }, [saving])
  useEscapeKey(!saving, onClose)
  const limits = [['maxDispatch', 'Maximum dispatches'], ['maxAttempts', 'Maximum attempts'], ['wallClockSeconds', 'Time limit in seconds']] as const
  const hint = inputHint.trim()
  const duration = readableDuration(inputs.limits.wallClockSeconds)
  return <div ref={dialogRef} className="pop board-dialog start-mission" role="dialog" aria-modal="true" aria-label="Start mission" onPointerDown={event => event.stopPropagation()}
    onKeyDown={event => {
      if (event.key !== 'Tab') return
      const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), a[href], [tabindex]'))
        .filter(element => element.tabIndex >= 0 && !element.closest('[hidden]'))
      const target = event.shiftKey ? controls[controls.length - 1] : controls[0]
      const boundary = event.shiftKey ? controls[0] : controls[controls.length - 1]
      if (document.activeElement === boundary) {
        event.preventDefault()
        target?.focus()
      }
    }}>
    <div className="pop-head">
      <span className="pt">Start {title}</span>
      <button className="x" type="button" aria-label="Close start mission" disabled={saving} onClick={onClose}>x</button>
    </div>
    <form className="pop-body" onSubmit={async event => {
      event.preventDefault()
      if (!event.currentTarget.reportValidity()) return
      setSaving(true)
      setError('')
      const limits = { maxDispatch: Number(inputs.limits.maxDispatch), maxAttempts: Number(inputs.limits.maxAttempts), wallClockSeconds: Number(inputs.limits.wallClockSeconds), redact: inputs.limits.redact }
      try { await onStart({ ...inputs, limits }, channel); onClose() } catch (err) { setError(err instanceof Error ? err.message : 'Failed to start run') } finally { setSaving(false) }
    }}>
      <label htmlFor="start-mission-cwd">Working directory</label>
      <input id="start-mission-cwd" className="f" required value={inputs.cwd} pattern="/.*" aria-describedby="start-mission-cwd-help"
        placeholder="/path/to/project" onChange={event => setInputs({ ...inputs, cwd: event.target.value })} />
      <p id="start-mission-cwd-help" className="field-note">The absolute path of the directory the agents work in.</p>
      <label htmlFor="start-mission-brief">Brief</label>
      <textarea id="start-mission-brief" required value={inputs.brief} aria-describedby="start-mission-brief-help"
        onChange={event => setInputs({ ...inputs, brief: event.target.value })} />
      {hint ? (
        <div id="start-mission-brief-help" className="field-note">
          <Suspense fallback={hint}><Markdown content={hint} className="start-mission-hint" /></Suspense>
        </div>
      ) : <p id="start-mission-brief-help" className="field-note">{DEFAULT_BRIEF_HINT}</p>}
      <label htmlFor="start-mission-bead">Bead</label>
      <input id="start-mission-bead" className="f" value={inputs.beadId} pattern="[A-Za-z0-9][A-Za-z0-9._-]*" aria-describedby="start-mission-bead-help"
        onChange={event => setInputs({ ...inputs, beadId: event.target.value })} />
      <p id="start-mission-bead-help" className="field-note">Optional. The Beads issue this run belongs to, for example form-3yd.4.</p>
      <span className="start-mission-label" aria-hidden="true">Human gates</span>
      <HumanChannelChoice value={channel} disabled={saving} describedBy="start-mission-channel-help" onChange={setChannel} />
      <p id="start-mission-channel-help" className="field-note">Saved on the mission when you start. {HUMAN_CHANNEL_TIMING}</p>
      <div className="start-mission-limits">
        {limits.map(([key, label]) => <div key={key}>
          <label htmlFor={`start-mission-${key}`}>{label}</label>
          <input id={`start-mission-${key}`} className="f" type="number" min="1" required value={inputs.limits[key]}
            aria-describedby={key === 'wallClockSeconds' ? `${duration ? 'start-mission-duration ' : ''}start-mission-time-limit-help` : `start-mission-${key}-help`}
            onChange={event => setInputs({ ...inputs, limits: { ...inputs.limits, [key]: event.target.value } })} />
          {key === 'wallClockSeconds'
            ? <p id="start-mission-duration" className="field-note">{duration}</p>
            : <p id={`start-mission-${key}-help`} className="field-note">{LIMIT_HINTS[key]}</p>}
        </div>)}
      </div>
      <p id="start-mission-time-limit-help" className="field-note">{TIME_LIMIT_HINT}</p>
      {error && <p className="field-note error" role="alert">{error}</p>}
      <div className="pop-actions">
        <button className="cancel" type="button" disabled={saving} onClick={onClose}>Cancel</button>
        <button className="save" type="submit" disabled={saving}>{saving ? 'Starting…' : 'Start mission'}</button>
      </div>
    </form>
  </div>
}
