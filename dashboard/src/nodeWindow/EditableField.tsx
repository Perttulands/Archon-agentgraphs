import { useState, type FormEvent, type KeyboardEvent, type ReactNode } from 'react'
import Markdown from '../evidence/Markdown'

/**
 * One field of a node, read and edited in the same place. It reads as text, or
 * as Markdown for the long fields, until Edit turns it into an input. Saving
 * sends one change; Escape or Cancel puts the text back.
 */
export function EditableField({ label, value, multiline = false, markdown = false, placeholder, hint, validate, onSave, children }: {
  label: string
  value: string
  multiline?: boolean
  markdown?: boolean
  placeholder: string
  hint?: string
  /** A reason the value cannot be saved, or '' when it can. */
  validate?: (value: string) => string
  /** Resolves true once the change is saved. */
  onSave: (value: string) => Promise<boolean>
  /** Drawn instead of the value while reading, for values that are not plain text. */
  children?: ReactNode
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [savedReceipt, setSavedReceipt] = useState(false)
  const name = label.toLowerCase()

  const cancel = () => {
    setDraft(null)
    setError('')
  }

  const save = async (event?: FormEvent) => {
    event?.preventDefault()
    if (draft === null || saving) return
    const next = draft.trim()
    if (next === value.trim()) {
      cancel()
      return
    }
    const problem = validate?.(next) || ''
    if (problem) {
      setError(problem)
      return
    }
    setSaving(true)
    const saved = await onSave(next)
    setSaving(false)
    if (saved) {
      cancel()
      setSavedReceipt(true)
    }
    else setError(`The ${name} was not saved.`)
  }

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      // Escape leaves the field, not the window.
      event.preventDefault()
      cancel()
    } else if (event.key === 'Enter' && multiline && (event.ctrlKey || event.metaKey)) {
      event.preventDefault()
      void save()
    }
  }

  return (
    <div className={`nfield${draft !== null ? ' editing' : ''}`}>
      <div className="nfield-head">
        <span className="nfield-label">{label}</span>
        {draft === null ? (
          <button type="button" className="nfield-edit" aria-label={`Edit ${name}`} onClick={() => { setSavedReceipt(false); setDraft(value) }}>Edit</button>
        ) : null}
      </div>
      {savedReceipt ? <p className="nfield-note" role="status">{label} saved.</p> : null}
      {draft !== null ? (
        <form className="nfield-form" onSubmit={event => void save(event)}>
          {multiline ? (
            <textarea aria-label={label} value={draft} autoFocus disabled={saving} rows={Math.min(18, Math.max(4, draft.split('\n').length + 1))}
              onChange={event => setDraft(event.target.value)} onKeyDown={onKeyDown} />
          ) : (
            <input aria-label={label} value={draft} autoFocus disabled={saving} spellCheck={!validate}
              onChange={event => { setDraft(event.target.value); setError('') }} onKeyDown={onKeyDown} />
          )}
          {error ? <p className="nfield-note error" role="alert">{error}</p> : hint ? <p className="nfield-note">{hint}</p> : null}
          <p className="nfield-note">{multiline ? 'Ctrl/Cmd+Enter to save · ' : ''}Esc to cancel</p>
          <div className="nfield-actions">
            <button type="button" onClick={cancel} disabled={saving}>Cancel</button>
            <button type="submit" className="primary" aria-label={`Save ${name}`} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </div>
        </form>
      ) : children ? (
        <div className="nfield-value">{children}</div>
      ) : value.trim() ? (
        markdown ? <Markdown content={value} className="nfield-value nfield-markdown" /> : <div className="nfield-value">{value}</div>
      ) : (
        <div className="nfield-value placeholder">{placeholder}</div>
      )}
    </div>
  )
}
