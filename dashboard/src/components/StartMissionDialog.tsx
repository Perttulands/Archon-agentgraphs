import { Suspense, lazy, useState } from 'react'
import { useEscapeKey } from './useEscapeKey'
import '../styles/formations-start-mission.css'

// The Markdown renderer is its own chunk; the hint reads as its source until it loads.
const Markdown = lazy(() => import('../evidence/Markdown'))

export interface RunInputs {
  cwd: string
  brief: string
  beadId: string
  limits: { maxDispatch: number; maxAttempts: number; wallClockSeconds: number; redact: boolean }
}

/** What the brief field asks for when the mission gives no input hint of its own. */
export const DEFAULT_BRIEF_HINT = 'The input this run works on: the request, sketch or task its first step receives. The board stays reusable; each run takes its own brief.'

export function StartMissionDialog({ title, beadId = '', inputHint = '', onStart, onClose }: {
  title: string; beadId?: string; inputHint?: string; onStart: (inputs: RunInputs) => Promise<void>; onClose: () => void
}) {
  const [inputs, setInputs] = useState<RunInputs>({ cwd: '', brief: '', beadId, limits: { maxDispatch: 20, maxAttempts: 3, wallClockSeconds: 1800, redact: false } })
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEscapeKey(!saving, onClose)
  const limits = [['maxDispatch', 'Maximum dispatches'], ['maxAttempts', 'Maximum attempts'], ['wallClockSeconds', 'Time limit in seconds']] as const
  const hint = inputHint.trim()
  return <div className="pop board-dialog start-mission" role="dialog" aria-modal="true" aria-label="Start mission" onPointerDown={event => event.stopPropagation()}>
    <div className="pop-head">
      <span className="pt">Start {title}</span>
      <button className="x" type="button" aria-label="Close start mission" disabled={saving} onClick={onClose}>x</button>
    </div>
    <form className="pop-body" onSubmit={async event => {
      event.preventDefault()
      setSaving(true)
      setError('')
      try { await onStart(inputs); onClose() } catch (err) { setError(err instanceof Error ? err.message : 'Failed to start run') } finally { setSaving(false) }
    }}>
      <label htmlFor="start-mission-cwd">Working directory</label>
      <input id="start-mission-cwd" className="f" required autoFocus value={inputs.cwd} pattern="/.*" aria-describedby="start-mission-cwd-help"
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
      <div className="start-mission-limits">
        {limits.map(([key, label]) => <div key={key}>
          <label htmlFor={`start-mission-${key}`}>{label}</label>
          <input id={`start-mission-${key}`} className="f" type="number" min="1" required value={inputs.limits[key]}
            onChange={event => setInputs({ ...inputs, limits: { ...inputs.limits, [key]: Number(event.target.value) } })} />
        </div>)}
      </div>
      {error && <p className="field-note error" role="alert">{error}</p>}
      <div className="pop-actions">
        <button className="cancel" type="button" disabled={saving} onClick={onClose}>Cancel</button>
        <button className="save" type="submit" disabled={saving}>{saving ? 'Starting…' : 'Start mission'}</button>
      </div>
    </form>
  </div>
}
