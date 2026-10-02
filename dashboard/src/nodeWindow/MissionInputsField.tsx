import { AuthoringReadOnly } from './EditableField'
import { useContext, useState, type FormEvent } from 'react'
import type { MissionInput } from '../components/formationsTypes'

const NAME = /^[a-z][a-z0-9_]{0,63}$/

/** Why a list of inputs cannot be saved, or '' when it can. Mirrors the daemon. */
export function inputsProblem(inputs: MissionInput[]): string {
  const seen = new Set<string>()
  for (const input of inputs) {
    const name = input.name.trim()
    if (!NAME.test(name)) return `Name ${name ? `"${name}"` : 'each input'}: a lowercase letter, then lowercase letters, digits or underscores.`
    if (seen.has(name)) return `Two inputs are named ${name}.`
    seen.add(name)
  }
  return ''
}

/**
 * The Input card's declared inputs, read in full and edited in place
 * (archon-o7p.3): a run supplies each one, and step briefs reference it as
 * {name}. None declared means one required text input, brief.
 */
export function MissionInputsField({ inputs, onSave }: {
  inputs: MissionInput[] | undefined
  /** Resolves true once the change is saved. */
  onSave: (inputs: MissionInput[]) => Promise<boolean>
}) {
  const readOnly = useContext(AuthoringReadOnly)
  const [draft, setDraft] = useState<MissionInput[] | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const declared = inputs || []
  const change = (index: number, patch: Partial<MissionInput>) => {
    setDraft(current => (current || []).map((input, at) => at === index ? { ...input, ...patch } : input))
    setError('')
  }
  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft || saving) return
    const next = draft.map(input => ({ ...input, name: input.name.trim(), description: (input.description || '').trim() }))
    const problem = inputsProblem(next)
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
    } else setError('The inputs were not saved.')
  }
  return (
    <div className={`nfield${draft ? ' editing' : ''}`}>
      <div className="nfield-head">
        <span className="nfield-label">Inputs</span>
        {draft || readOnly ? null : <button type="button" className="nfield-edit" aria-label="Edit inputs" onClick={() => { setSaved(false); setDraft(declared.map(input => ({ ...input }))) }}>Edit</button>}
      </div>
      {saved ? <p className="nfield-note" role="status">Inputs saved.</p> : null}
      {draft ? (
        <form className="nfield-form" onSubmit={event => void save(event)}
          onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); setDraft(null); setError('') } }}>
          {draft.map((input, index) => (
            <fieldset key={index} className="ninput-edit" aria-label={`Input ${index + 1}`}>
              <div className="ninput-row">
                <input aria-label={`Name of input ${index + 1}`} value={input.name} placeholder="name" spellCheck={false} disabled={saving}
                  autoFocus={index === draft.length - 1 && !input.name} onChange={event => change(index, { name: event.target.value })} />
                <select aria-label={`Kind of input ${index + 1}`} value={input.kind} disabled={saving}
                  onChange={event => change(index, { kind: event.target.value as MissionInput['kind'] })}>
                  <option value="text">text</option>
                  <option value="file">file</option>
                  <option value="folder">folder</option>
                </select>
                <label className="ninput-required">
                  <input type="checkbox" checked={!!input.required} disabled={saving} onChange={event => change(index, { required: event.target.checked })} /> required
                </label>
                <button type="button" aria-label={`Remove input ${input.name || index + 1}`} disabled={saving}
                  onClick={() => setDraft(current => (current || []).filter((_, at) => at !== index))}>Remove</button>
              </div>
              <input aria-label={`Description of input ${index + 1}`} value={input.description || ''} placeholder="What the run should supply" disabled={saving}
                onChange={event => change(index, { description: event.target.value })} />
            </fieldset>
          ))}
          <div className="nfield-actions ninput-add">
            <button type="button" disabled={saving} onClick={() => setDraft(current => [...(current || []), { name: '', kind: 'text', required: true }])}>Add input</button>
          </div>
          {error ? <p className="nfield-note error" role="alert">{error}</p>
            : <p className="nfield-note">Step briefs reference an input as {'{name}'}. With none, each run takes a required brief.</p>}
          <p className="nfield-note">Esc to cancel</p>
          <div className="nfield-actions">
            <button type="button" disabled={saving} onClick={() => { setDraft(null); setError('') }}>Cancel</button>
            <button type="submit" className="primary" aria-label="Save inputs" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </div>
        </form>
      ) : declared.length ? (
        <ul className="nwin-list ninput-list" aria-label="Inputs">
          {declared.map(input => (
            <li key={input.name}>
              <code>{`{${input.name}}`}</code> {input.kind}{input.required ? ', required' : ', optional'}
              {input.description ? <span className="ninput-description"> · {input.description}</span> : null}
            </li>
          ))}
        </ul>
      ) : (
        <div className="nfield-value placeholder">No inputs declared. Each run takes a required brief.</div>
      )}
    </div>
  )
}
