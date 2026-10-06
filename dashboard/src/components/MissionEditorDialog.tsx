/* Input card creator. An existing Input card is read and edited in its node
 * window. Every field is optional. */
import { useState } from 'react'

export interface MissionDraft {
  title: string
  goal: string
}

export function MissionEditorDialog({ initial, saving, onSave, onClose }: {
  initial: MissionDraft
  saving: boolean
  onSave: (draft: MissionDraft) => void
  onClose: () => void
}) {
  const [draft, setDraft] = useState<MissionDraft>(initial)
  const heading = 'Add Input card'
  return (
    <div className="pop" role="dialog" aria-label={heading} onPointerDown={event => event.stopPropagation()}>
      <div className="pop-head">
        <span className="pt">{heading}</span>
        <button className="x" type="button" aria-label="Close Input card creator" disabled={saving} onClick={onClose}>x</button>
      </div>
      <form
        className="pop-body"
        onSubmit={event => {
          event.preventDefault()
          onSave({ title: draft.title.trim(), goal: draft.goal.trim() })
        }}
      >
        <label htmlFor="cockpit-mission-title">Title</label>
        <input id="cockpit-mission-title" autoFocus onFocus={event => { if (!event.currentTarget.dataset.caretReady) { event.currentTarget.select(); event.currentTarget.dataset.caretReady="1" } }} className="f" aria-label="Input card title" value={draft.title}
          onChange={event => setDraft(current => ({ ...current, title: event.target.value }))} />
        <label htmlFor="cockpit-mission-goal">Goal</label>
        <textarea id="cockpit-mission-goal" aria-label="Mission goal" value={draft.goal}
          onChange={event => setDraft(current => ({ ...current, goal: event.target.value }))} />
        <div className="pop-actions">
          <button className="cancel" type="button" aria-label="Cancel adding the Input card" disabled={saving} onClick={onClose}>Cancel</button>
          <button className="save" type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Add Input card'}
          </button>
        </div>
      </form>
    </div>
  )
}
