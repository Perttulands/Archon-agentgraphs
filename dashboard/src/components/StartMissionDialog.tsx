import { useState } from 'react'

export interface RunInputs {
  cwd: string
  brief: string
  beadId: string
  limits: { maxDispatch: number; maxAttempts: number; wallClockSeconds: number; redact: boolean }
}

export function StartMissionDialog({ title, beadId = '', onStart, onClose }: {
  title: string; beadId?: string; onStart: (inputs: RunInputs) => Promise<void>; onClose: () => void
}) {
  const [inputs, setInputs] = useState<RunInputs>({ cwd: '', brief: '', beadId, limits: { maxDispatch: 20, maxAttempts: 3, wallClockSeconds: 1800, redact: false } })
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  return <div className="pop board-dialog" role="dialog" aria-modal="true" aria-label="Start mission" onPointerDown={event => event.stopPropagation()}>
    <div className="pop-head"><span className="pt">Start {title}</span></div>
    <form className="pop-body" onSubmit={async event => {
      event.preventDefault()
      setSaving(true)
      setError('')
      try { await onStart(inputs); onClose() } catch (err) { setError(err instanceof Error ? err.message : 'Failed to start run') } finally { setSaving(false) }
    }}>
      <label>Working directory<input required autoFocus value={inputs.cwd} pattern="/.*" onChange={event => setInputs({ ...inputs, cwd: event.target.value })} /></label>
      <label>Brief<textarea required value={inputs.brief} onChange={event => setInputs({ ...inputs, brief: event.target.value })} /></label>
      <label>Bead<input value={inputs.beadId} pattern="[A-Za-z0-9][A-Za-z0-9._-]*" onChange={event => setInputs({ ...inputs, beadId: event.target.value })} /></label>
      {([['maxDispatch', 'Maximum dispatches'], ['maxAttempts', 'Maximum attempts'], ['wallClockSeconds', 'Time limit in seconds']] as const).map(([key, label]) => <label key={key}>{label}<input type="number" min="1" required value={inputs.limits[key]} onChange={event => setInputs({ ...inputs, limits: { ...inputs.limits, [key]: Number(event.target.value) } })} /></label>)}
      {error && <p role="alert">{error}</p>}
      <div className="pop-actions"><button type="button" disabled={saving} onClick={onClose}>Cancel</button><button type="submit" disabled={saving}>{saving ? 'Starting…' : 'Start mission'}</button></div>
    </form>
  </div>
}
