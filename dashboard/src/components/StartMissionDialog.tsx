import { Suspense, lazy, useLayoutEffect, useRef, useState } from 'react'
import { useEscapeKey } from './useEscapeKey'
import { HumanChannelChoice } from '../humanChannel/HumanChannelChoice'
import { HUMAN_CHANNEL_TIMING, type HumanChannel } from '../humanChannel/humanChannel'
import type { MissionInput } from './formationsTypes'
import { ApiRequestError } from './formationsApi'
import { DEFAULT_BRIEF_HINT, inputLabel, missingInputs, missionRunInputs, suppliedInputs } from './missionInputs'
import '../styles/formations-start-mission.css'

export { DEFAULT_BRIEF_HINT }

// The Markdown renderer is its own chunk; a description reads as its source until it loads.
const Markdown = lazy(() => import('../evidence/Markdown'))

/** The run fields a start sends: a mission run and a single step take the same ones. */
export interface RunInputs {
  cwd: string
  contextPaths: string[]
  beadId: string
  /** The mission's inputs by name; blank ones are left out. */
  inputs: Record<string, string>
}

const PATH_HELP: Record<string, string> = {
  file: 'The absolute path of a file on the agent host.',
  folder: 'The absolute path of a directory on the agent host.',
}

/** Starts a mission from its Input card, or one step on its own (▶), asking
 *  for the mission's declared inputs (archon-o7p.4). */
export function StartMissionDialog({ title, step, inputs = missionRunInputs(undefined), humanChannel = 'notify', onStart, onClose }: {
  title: string
  /** The step's title when ▶ runs it on its own; a single step meets no human gate. */
  step?: string
  /** The inputs a run supplies, from missionRunInputs. */
  inputs?: MissionInput[]
  /** The mission's human channel; a different choice is saved on the mission before the run starts. */
  humanChannel?: HumanChannel
  onStart: (run: RunInputs, humanChannel: HumanChannel) => Promise<void>; onClose: () => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [missing, setMissing] = useState<string[]>([])
  const [fieldProblems, setFieldProblems] = useState<Record<string, string>>({})
  const [fields, setFields] = useState({ cwd: '', beadId: '' })
  const [contextPaths, setContextPaths] = useState('')
  const [workspaceMode, setWorkspaceMode] = useState('automatic')
  const dialogRef = useRef<HTMLDivElement>(null)
  const firstField = `start-input-${inputs[0]?.name}`
  useLayoutEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    document.getElementById(firstField)?.focus()
    return () => { if (opener?.isConnected) opener.focus() }
  }, [firstField])
  const [channel, setChannel] = useState<HumanChannel>(humanChannel)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<{ message: string; details: string[] } | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  // A refusal shows at the foot of a long form; bring it into view.
  useLayoutEffect(() => { if (error) errorRef.current?.scrollIntoView?.({ block: 'nearest' }) }, [error])
  useLayoutEffect(() => {
    if (!saving) return
    const dialog = dialogRef.current
    const focused = document.activeElement
    // Disabling the submit button can move browser focus to the document body.
    if (dialog && (!dialog.contains(focused) || focused?.matches(':disabled'))) document.getElementById(firstField)?.focus()
  }, [saving, firstField])
  useEscapeKey(!saving, onClose)
  const action = step ? 'Run step' : 'Start mission'
  const setValue = (name: string, value: string) => {
    setValues(current => ({ ...current, [name]: value }))
    if (value.trim()) setMissing(current => current.filter(item => item !== name))
  }
  return <div ref={dialogRef} className="pop board-dialog start-mission" role="dialog" aria-modal="true" aria-label={action} onPointerDown={event => event.stopPropagation()}
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
      <span className="pt">{step ? `Run ${step} on its own` : `Start ${title}`}</span>
      <button className="x" type="button" aria-label={`Close ${action.toLowerCase()}`} disabled={saving} onClick={onClose}>x</button>
    </div>
    <form className="pop-body" noValidate onSubmit={async event => {
      event.preventDefault()
      const blank = missingInputs(inputs, values)
      setMissing(blank)
      if (blank.length) {
        document.getElementById(`start-input-${blank[0]}`)?.focus()
        return
      }
      const problems: Record<string, string> = {}
      for (const control of Array.from(event.currentTarget.querySelectorAll<HTMLInputElement>('input'))) {
        if (!control.validity.valid) problems[control.id] = control.id === 'start-mission-bead'
          ? 'Use letters, digits, dots, underscores and hyphens, starting with a letter or digit, for example archon-3yd.4.'
          : 'Use an absolute path on the agent host, starting with /.'
      }
      setFieldProblems(problems)
      if (Object.keys(problems).length) { document.getElementById(Object.keys(problems)[0])?.focus(); return }
      setSaving(true)
      setError(null)
      try {
        await onStart({
          cwd: workspaceMode === 'existing' ? fields.cwd : '',
          contextPaths: contextPaths.split(/\r?\n/).map(path => path.trim()).filter(Boolean),
          beadId: fields.beadId,
          inputs: suppliedInputs(inputs, values),
        }, channel)
        onClose()
      } catch (err) {
        const details = err instanceof ApiRequestError ? err.findings.map(finding => finding.message) : []
        setError({ message: err instanceof Error ? err.message : 'Failed to start run', details })
      } finally { setSaving(false) }
    }}>
      {step && <p className="field-note">{step} runs alone, with the mission&rsquo;s inputs, in {title}.</p>}
      {inputs.map(input => <MissionInputField key={input.name} input={input} value={values[input.name] || ''} missing={missing.includes(input.name)} error={fieldProblems[`start-input-${input.name}`]} onChange={value => setValue(input.name, value)} />)}
      <label htmlFor="start-mission-workspace">Workspace</label>
      <select id="start-mission-workspace" className="f" value={workspaceMode} disabled={saving}
        onChange={event => setWorkspaceMode(event.target.value)}>
        <option value="automatic">Create a workspace for this run</option>
        <option value="existing">Use an existing project</option>
      </select>
      {workspaceMode === 'existing' && <>
        <label htmlFor="start-mission-cwd">Working directory</label>
        <input id="start-mission-cwd" className="f" required value={fields.cwd} pattern="/.*" aria-describedby="start-mission-cwd-help"
          placeholder="/path/to/project" onChange={event => setFields({ ...fields, cwd: event.target.value })} />
        {fieldProblems['start-mission-cwd'] && <p className="field-note error" role="alert">{fieldProblems['start-mission-cwd']}</p>}
        <p id="start-mission-cwd-help" className="field-note">The absolute path of the directory the agents work in.</p>
      </>}
      <label htmlFor="start-mission-context">Context paths</label>
      <textarea id="start-mission-context" className="start-mission-context" value={contextPaths} aria-describedby="start-mission-context-help"
        onChange={event => setContextPaths(event.target.value)} />
      <p id="start-mission-context-help" className="field-note">Optional. One absolute path per line to an existing file or directory on the agent host. The first formation will inspect these paths. Your workspace choice does not change them.</p>
      <label htmlFor="start-mission-bead">Bead</label>
      <input id="start-mission-bead" className="f" value={fields.beadId} pattern="[A-Za-z0-9][A-Za-z0-9._-]*" aria-describedby="start-mission-bead-help"
        onChange={event => setFields({ ...fields, beadId: event.target.value })} />
      {fieldProblems['start-mission-bead'] && <p className="field-note error" role="alert">{fieldProblems['start-mission-bead']}</p>}
      <p id="start-mission-bead-help" className="field-note">Optional. The Beads issue this run belongs to, for example archon-3yd.4.</p>
      {!step && <>
        <span className="start-mission-label" aria-hidden="true">Human gates</span>
        <HumanChannelChoice value={channel} disabled={saving} describedBy="start-mission-channel-help" onChange={setChannel} />
        <p id="start-mission-channel-help" className="field-note">Saved on the mission when you start. {HUMAN_CHANNEL_TIMING}</p>
      </>}
      {error && <div ref={errorRef} className="field-note error" role="alert">
        {error.message} <button type="button" onClick={() => setError(null)}>Dismiss</button>
        {error.details.length > 0 && <ul className="start-mission-problems">{error.details.map(detail => <li key={detail}>{detail}</li>)}</ul>}
      </div>}
      <div className="pop-actions">
        <button className="cancel" type="button" disabled={saving} onClick={onClose}>Cancel</button>
        <button className="save" type="submit" disabled={saving}>{saving ? 'Starting…' : action}</button>
      </div>
    </form>
  </div>
}

/** One declared input: text in a text box, a file or folder as an absolute path. */
function MissionInputField({ input, value, missing, error, onChange }: {
  input: MissionInput; value: string; missing: boolean; error?: string; onChange: (value: string) => void
}) {
  const id = `start-input-${input.name}`
  const label = inputLabel(input.name)
  const description = (input.description || '').trim() || (input.name === 'brief' && input.kind === 'text' ? DEFAULT_BRIEF_HINT : '')
  const pathHelp = PATH_HELP[input.kind]
  const describedBy = [description && `${id}-help`, pathHelp && `${id}-path`, missing && `${id}-missing`].filter(Boolean).join(' ') || undefined
  return <>
    <label htmlFor={id}>{label}{input.required ? '' : ' (optional)'}</label>
    {input.kind === 'text'
      ? <textarea id={id} value={value} aria-required={input.required || undefined} aria-invalid={missing || undefined} aria-describedby={describedBy}
        onChange={event => onChange(event.target.value)} />
      : <input id={id} className="f" value={value} pattern="\s*/.*" aria-required={input.required || undefined} aria-invalid={missing || undefined} aria-describedby={describedBy}
        placeholder={input.kind === 'folder' ? '/path/to/directory' : '/path/to/file'} onChange={event => onChange(event.target.value)} />}
    {description && <div id={`${id}-help`} className="field-note">
      <Suspense fallback={description}><Markdown content={description} className="start-mission-hint" /></Suspense>
    </div>}
    {error && <p className="field-note error" role="alert">{error}</p>}
    {pathHelp && <p id={`${id}-path`} className="field-note">{pathHelp}</p>}
    {missing && <p id={`${id}-missing`} className="field-note error">Fill in {label.toLowerCase()} to start.</p>}
  </>
}
