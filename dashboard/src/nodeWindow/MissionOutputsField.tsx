import { AuthoringReadOnly } from './EditableField'
import { useContext, useState, type FormEvent } from 'react'
import type { BoardDocument, MissionOutput, PortKind } from '../components/formationsTypes'
import { portKindOf } from '../components/portKind'

const NAME = /^[a-z][a-z0-9_]{0,63}$/

/** A step output port an output can be bound to: "<step-id>:<port-id>", named "<step title> · <port label>". */
export interface OutputPortChoice {
  value: string
  label: string
  kind: PortKind
}

/** Every step output port on the board, in board order. */
export function outputPortChoices(board: Pick<BoardDocument, 'formations'>): OutputPortChoice[] {
  return board.formations.flatMap(formation => formation.outputs.map(port => ({
    value: `${formation.id}:${port.id}`,
    label: `${formation.title || 'Untitled formation'} · ${port.label || port.id}`,
    kind: portKindOf(port),
  })))
}

/** Why a list of outputs cannot be saved, or '' when it can. Mirrors the daemon; an unbound output is a draft. */
export function outputsProblem(outputs: MissionOutput[]): string {
  const seen = new Set<string>()
  for (const output of outputs) {
    const name = output.name.trim()
    if (!NAME.test(name)) return `Name ${name ? `"${name}"` : 'each output'}: a lowercase letter, then lowercase letters, digits or underscores.`
    if (seen.has(name)) return `Two outputs are named ${name}.`
    seen.add(name)
  }
  return ''
}

/**
 * The Input card's declared outputs: what a succeeded run delivers, each bound
 * to the step output port that carries it. Edited in place like the inputs.
 */
export function MissionOutputsField({ outputs, ports, onSave }: {
  outputs: MissionOutput[] | undefined
  ports: OutputPortChoice[]
  /** Resolves true once the change is saved. */
  onSave: (outputs: MissionOutput[]) => Promise<boolean>
}) {
  const readOnly = useContext(AuthoringReadOnly)
  const [draft, setDraft] = useState<MissionOutput[] | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const declared = outputs || []
  const portLabel = (from: string | undefined) => ports.find(port => port.value === from)?.label || from || ''
  const change = (index: number, patch: Partial<MissionOutput>) => {
    setDraft(current => (current || []).map((output, at) => at === index ? { ...output, ...patch } : output))
    setError('')
  }
  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft || saving) return
    const next = draft.map(output => ({ ...output, name: output.name.trim(), description: (output.description || '').trim(), from: output.from || '' }))
    const problem = outputsProblem(next)
    if (problem) {
      setError(problem)
      return
    }
    setSaving(true)
    let ok: boolean
    try { ok = await onSave(next) }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); return }
    finally { setSaving(false) }
    if (ok) {
      setDraft(null)
      setSaved(true)
    } else setError('The outputs were not saved.')
  }
  return (
    <div className={`nfield${draft ? ' editing' : ''}`}>
      <div className="nfield-head">
        <span className="nfield-label">Outputs</span>
        {draft || readOnly ? null : <button type="button" className="nfield-edit" aria-label="Edit outputs" onClick={() => { setSaved(false); setDraft(declared.map(output => ({ ...output }))) }}>Edit</button>}
      </div>
      {saved ? <p className="nfield-note" role="status">Outputs saved.</p> : null}
      {draft ? (
        <form className="nfield-form" onSubmit={event => void save(event)}
          onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); setDraft(null); setError('') } }}>
          {draft.map((output, index) => {
            // A port that is no longer on the board stays selectable so the draft shows what it names.
            const missing = output.from && !ports.some(port => port.value === output.from)
            return (
              <fieldset key={index} className="ninput-edit" aria-label={`Output ${index + 1}`}>
                <div className="ninput-row">
                  <input aria-label={`Name of output ${index + 1}`} value={output.name} placeholder="name" spellCheck={false} disabled={saving}
                    autoFocus={index === draft.length - 1 && !output.name} onChange={event => change(index, { name: event.target.value })} />
                  <select aria-label={`Kind of output ${index + 1}`} value={output.kind} disabled={saving}
                    onChange={event => change(index, { kind: event.target.value as PortKind })}>
                    <option value="text">text</option>
                    <option value="file">file</option>
                    <option value="folder">folder</option>
                  </select>
                  <label className="ninput-required">
                    <input type="checkbox" checked={!!output.required} disabled={saving} onChange={event => change(index, { required: event.target.checked })} /> required
                  </label>
                  <button type="button" aria-label={`Remove output ${output.name || index + 1}`} disabled={saving}
                    onClick={() => setDraft(current => (current || []).filter((_, at) => at !== index))}>Remove</button>
                </div>
                <select aria-label={`Port of output ${index + 1}`} value={output.from || ''} disabled={saving}
                  onChange={event => {
                    // Binding takes the port's kind, which the output must match.
                    const port = ports.find(choice => choice.value === event.target.value)
                    change(index, { from: event.target.value, ...(port ? { kind: port.kind } : {}) })
                  }}>
                  <option value="">Not bound to a step output</option>
                  {missing ? <option value={output.from}>{output.from} (not on the board)</option> : null}
                  {ports.map(port => <option key={port.value} value={port.value}>{port.label}</option>)}
                </select>
                <input aria-label={`Description of output ${index + 1}`} value={output.description || ''} placeholder="What the run delivers" disabled={saving}
                  onChange={event => change(index, { description: event.target.value })} />
              </fieldset>
            )
          })}
          <div className="nfield-actions ninput-add">
            <button type="button" disabled={saving} onClick={() => setDraft(current => [...(current || []), { name: '', kind: 'text', required: true, from: '' }])}>Add output</button>
          </div>
          {error ? <p className="nfield-note error" role="alert">{error}</p> : null}
          <p className="nfield-note">Esc to cancel</p>
          <div className="nfield-actions">
            <button type="button" disabled={saving} onClick={() => { setDraft(null); setError('') }}>Cancel</button>
            <button type="submit" className="primary" aria-label="Save outputs" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </div>
        </form>
      ) : declared.length ? (
        <ul className="nwin-list ninput-list" aria-label="Outputs">
          {declared.map(output => (
            <li key={output.name}>
              <code>{output.name}</code> {output.kind}{output.required ? ', required' : ', optional'}
              {' · '}{output.from ? `from ${portLabel(output.from)}` : 'not bound'}
              {output.description ? <span className="ninput-description"> · {output.description}</span> : null}
            </li>
          ))}
        </ul>
      ) : (
        <div className="nfield-value placeholder">No outputs declared.</div>
      )}
    </div>
  )
}
