import { useCallback, useEffect, useState } from 'react'
import { fetchAgentCard, overrideAgentCard } from './formationsApi'

// The one persona editor. The Agents tab opens it from the persona inspector
// and the Boards roster opens it from an agent's ••• action; both save the same
// persona override. A role is role text: it carries no harness, model or
// effort.

export interface PersonaEditorTarget {
  id: string
  displayName?: string
  kind?: string
  tags?: string[]
  preset?: boolean
  customized?: boolean
  unbound?: boolean
}

interface EditorState {
  preset: boolean
  customized: boolean
  displayName: string
  kind: string
  summary: string
  capabilities: string
  etag: string
  loading: boolean
  saving: boolean
  error: string
}

const capabilityTags = (tags: string[] | undefined) => (tags || []).filter(tag => !tag.includes(':')).join(', ')

export default function PersonaEditorDialog({ agent, returnFocus, onClose, onSaved }: {
  agent: PersonaEditorTarget
  /** Receives focus again when the dialog closes. */
  returnFocus?: HTMLElement | null
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [editor, setEditor] = useState<EditorState>(() => ({
    preset: Boolean(agent.preset),
    customized: Boolean(agent.customized),
    displayName: agent.displayName || agent.id,
    kind: agent.kind || (agent.unbound ? 'unbound' : 'agent'),
    summary: '',
    capabilities: capabilityTags(agent.tags),
    etag: '',
    loading: true,
    saving: false,
    error: '',
  }))

  const close = useCallback(() => {
    onClose()
    window.setTimeout(() => { if (returnFocus?.isConnected) returnFocus.focus() }, 0)
  }, [onClose, returnFocus])

  useEffect(() => {
    let current = true
    fetchAgentCard(agent.id).then(card => {
      if (!current) return
      setEditor(state => ({
        ...state,
        preset: Boolean(card.preset),
        customized: Boolean(card.customized),
        displayName: card.displayName || card.id,
        kind: card.kind,
        summary: card.summary || '',
        capabilities: capabilityTags(card.tags),
        etag: card.etag,
        loading: false,
      }))
    }, err => {
      if (current) setEditor(state => ({ ...state, loading: false, error: err instanceof Error ? err.message : 'Failed to load agent card' }))
    })
    return () => { current = false }
  }, [agent.id])

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || editor.saving) return
      event.preventDefault()
      close()
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [close, editor.saving])

  const save = async () => {
    if (editor.loading || !editor.etag) return
    setEditor(state => ({ ...state, saving: true, error: '' }))
    try {
      await overrideAgentCard(agent.id, editor.etag, {
        displayName: editor.displayName.trim(),
        kind: editor.kind.trim(),
        summary: editor.summary.trim(),
        capabilities: editor.capabilities.split(',').map(value => value.trim()).filter(Boolean),
      })
      await onSaved()
      close()
    } catch (err) {
      setEditor(state => ({ ...state, saving: false, error: err instanceof Error ? err.message : 'Failed to save agent override' }))
    }
  }

  const field = (key: 'displayName' | 'kind' | 'summary' | 'capabilities') =>
    (event: { target: { value: string } }) => setEditor(state => ({ ...state, [key]: event.target.value }))

  return (
    <div
      className="pop agent-dialog"
      role="dialog"
      aria-modal="true"
      aria-label={editor.preset ? 'Edit agent preset' : 'Edit agent'}
      data-testid="persona-editor"
      onPointerDown={event => event.stopPropagation()}
    >
      <form onSubmit={event => { event.preventDefault(); void save() }}>
        <div className="phd">
          <span>{editor.preset ? 'Built-in role override' : 'Agent override'}</span>
          <button autoFocus={editor.loading} type="button" className="x" aria-label="Close agent editor" disabled={editor.saving} onClick={close}>×</button>
        </div>
        <div className="agent-dialog-id">{agent.id}{editor.customized ? ' · customized' : ' · built-in default'}</div>
        {editor.loading ? <div className="agent-dialog-loading">Loading persona card…</div> : (
          <div className="agent-dialog-fields">
            <label>
              <span>Display name</span>
              <input autoFocus aria-label="Agent display name" value={editor.displayName} onChange={field('displayName')} />
            </label>
            <label>
              <span>Role</span>
              <input aria-label="Agent role" value={editor.kind} onChange={field('kind')} />
            </label>
            <label className="wide">
              <span>Summary</span>
              <textarea aria-label="Agent summary" value={editor.summary} onChange={field('summary')} />
            </label>
            <label className="wide">
              <span>Capabilities</span>
              <input aria-label="Agent capabilities" value={editor.capabilities} placeholder="implement, test, review" onChange={field('capabilities')} />
            </label>
          </div>
        )}
        {editor.preset ? <div className="agent-dialog-note">Saving materializes a local persona TOML override; the built-in default remains the fallback.</div> : null}
        {editor.error ? <div className="dialog-error" role="alert">{editor.error}</div> : null}
        <div className="board-dialog-actions">
          <button type="button" disabled={editor.saving} onClick={close}>Cancel</button>
          <button className="primary" type="submit" aria-label="Save agent override" disabled={editor.loading || editor.saving || !editor.etag}>{editor.saving ? 'Saving…' : 'Save override'}</button>
        </div>
      </form>
    </div>
  )
}
