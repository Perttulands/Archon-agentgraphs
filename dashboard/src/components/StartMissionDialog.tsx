import { Suspense, lazy, useLayoutEffect, useRef, useState } from 'react'
import { useEscapeKey } from './useEscapeKey'
import { HumanChannelChoice } from '../humanChannel/HumanChannelChoice'
import { HUMAN_CHANNEL_TIMING, type HumanChannel } from '../humanChannel/humanChannel'
import '../styles/formations-start-mission.css'

// The Markdown renderer is its own chunk; the hint reads as its source until it loads.
const Markdown = lazy(() => import('../evidence/Markdown'))

export interface RunInputs {
  cwd: string
  contextPaths: string[]
  brief: string
  beadId: string
}

/** What the brief field asks for when the mission gives no input hint of its own. */
export const DEFAULT_BRIEF_HINT = 'The input this run works on: the request, sketch or task its first step receives. The board stays reusable; each run takes its own brief.'

export function StartMissionDialog({ title, beadId = '', inputHint = '', humanChannel = 'notify', onStart, onClose }: {
  title: string; beadId?: string; inputHint?: string
  /** The mission's human channel; a different choice is saved on the mission before the run starts. */
  humanChannel?: HumanChannel
  onStart: (inputs: RunInputs, humanChannel: HumanChannel) => Promise<void>; onClose: () => void
}) {
  const [inputs, setInputs] = useState({ cwd: '', brief: '', beadId })
  const [contextPaths, setContextPaths] = useState('')
  const [workspaceMode, setWorkspaceMode] = useState('automatic')
  const dialogRef = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialogRef.current?.querySelector<HTMLTextAreaElement>('#start-mission-brief')?.focus()
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
      dialog.querySelector<HTMLTextAreaElement>('#start-mission-brief')?.focus()
    }
  }, [saving])
  useEscapeKey(!saving, onClose)
  const hint = inputHint.trim()
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
      try { await onStart({ ...inputs, contextPaths: contextPaths.split(/\r?\n/).map(path => path.trim()).filter(Boolean), cwd: workspaceMode === 'existing' ? inputs.cwd : '' }, channel); onClose() } catch (err) { setError(err instanceof Error ? err.message : 'Failed to start run') } finally { setSaving(false) }
    }}>
      <label htmlFor="start-mission-workspace">Workspace</label>
      <select id="start-mission-workspace" className="f" value={workspaceMode} disabled={saving}
        onChange={event => setWorkspaceMode(event.target.value)}>
        <option value="automatic">Create a workspace for this mission</option>
        <option value="existing">Use an existing project</option>
      </select>
      {workspaceMode === 'existing' && <>
        <label htmlFor="start-mission-cwd">Working directory</label>
        <input id="start-mission-cwd" className="f" required value={inputs.cwd} pattern="/.*" aria-describedby="start-mission-cwd-help"
          placeholder="/path/to/project" onChange={event => setInputs({ ...inputs, cwd: event.target.value })} />
        <p id="start-mission-cwd-help" className="field-note">The absolute path of the directory the agents work in.</p>
      </>}
      <label htmlFor="start-mission-brief">Brief</label>
      <textarea id="start-mission-brief" required value={inputs.brief} aria-describedby="start-mission-brief-help"
        onChange={event => setInputs({ ...inputs, brief: event.target.value })} />
      {hint ? (
        <div id="start-mission-brief-help" className="field-note">
          <Suspense fallback={hint}><Markdown content={hint} className="start-mission-hint" /></Suspense>
        </div>
      ) : <p id="start-mission-brief-help" className="field-note">{DEFAULT_BRIEF_HINT}</p>}
      <label htmlFor="start-mission-context">Context paths</label>
      <textarea id="start-mission-context" className="start-mission-context" value={contextPaths} aria-describedby="start-mission-context-help"
        onChange={event => setContextPaths(event.target.value)} />
      <p id="start-mission-context-help" className="field-note">Optional. One absolute path per line to an existing file or directory on the agent host. The first formation will inspect these paths. Your workspace choice does not change them.</p>
      <label htmlFor="start-mission-bead">Bead</label>
      <input id="start-mission-bead" className="f" value={inputs.beadId} pattern="[A-Za-z0-9][A-Za-z0-9._-]*" aria-describedby="start-mission-bead-help"
        onChange={event => setInputs({ ...inputs, beadId: event.target.value })} />
      <p id="start-mission-bead-help" className="field-note">Optional. The Beads issue this run belongs to, for example form-3yd.4.</p>
      <span className="start-mission-label" aria-hidden="true">Human gates</span>
      <HumanChannelChoice value={channel} disabled={saving} describedBy="start-mission-channel-help" onChange={setChannel} />
      <p id="start-mission-channel-help" className="field-note">Saved on the mission when you start. {HUMAN_CHANNEL_TIMING}</p>
      {error && <p className="field-note error" role="alert">{error}</p>}
      <div className="pop-actions">
        <button className="cancel" type="button" disabled={saving} onClick={onClose}>Cancel</button>
        <button className="save" type="submit" disabled={saving}>{saving ? 'Starting…' : 'Start mission'}</button>
      </div>
    </form>
  </div>
}
