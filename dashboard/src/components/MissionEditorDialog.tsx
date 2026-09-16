/* Mission creator. An existing mission is read and edited in its node window.
 * Every field is optional; a Bead ID that is given must be a safe issue ID. */
import { useState } from 'react'
import { isSafeBeadsIssueID } from './formationsBeadId'

export interface MissionDraft {
  title: string
  goal: string
  beadId: string
}

export function MissionEditorDialog({ initial, saving, onSave, onClose }: {
  initial: MissionDraft
  saving: boolean
  onSave: (draft: MissionDraft) => void
  onClose: () => void
}) {
  const [draft, setDraft] = useState<MissionDraft>(initial)
  const [error, setError] = useState('')
  const heading = 'Create mission'
  return (
    <div className="pop" role="dialog" aria-label={heading} onPointerDown={event => event.stopPropagation()}>
      <div className="pop-head">
        <span className="pt">{heading}</span>
        <button className="x" type="button" aria-label="Close mission creator" disabled={saving} onClick={onClose}>x</button>
      </div>
      <form
        className="pop-body"
        onSubmit={event => {
          event.preventDefault()
          const next = { title: draft.title.trim(), goal: draft.goal.trim(), beadId: draft.beadId.trim() }
          if (next.beadId && !isSafeBeadsIssueID(next.beadId)) {
            setError('Enter a Beads issue ID such as ctx-ug7.25, or leave it blank.')
            return
          }
          onSave(next)
        }}
      >
        <label htmlFor="cockpit-mission-title">Title</label>
        <input id="cockpit-mission-title" className="f" aria-label="Mission title" value={draft.title}
          onChange={event => setDraft(current => ({ ...current, title: event.target.value }))} />
        <label htmlFor="cockpit-mission-goal">Goal</label>
        <textarea id="cockpit-mission-goal" aria-label="Mission goal" value={draft.goal}
          onChange={event => setDraft(current => ({ ...current, goal: event.target.value }))} />
        <label htmlFor="cockpit-mission-bead">Bead ID</label>
        <input
          id="cockpit-mission-bead"
          className="f"
          aria-label="Mission Bead ID"
          aria-invalid={error ? true : undefined}
          aria-describedby="cockpit-mission-bead-help"
          autoCapitalize="none"
          spellCheck={false}
          value={draft.beadId}
          onChange={event => {
            const beadId = event.target.value
            setDraft(current => ({ ...current, beadId }))
            setError('')
          }}
        />
        <p id="cockpit-mission-bead-help" className={`field-note${error ? ' error' : ''}`} role={error ? 'alert' : undefined}>
          {error || 'Optional. The Beads issue that owns this mission, for example ctx-ug7.25.'}
        </p>
        <div className="pop-actions">
          <button className="cancel" type="button" aria-label="Cancel mission creation" disabled={saving} onClick={onClose}>Cancel</button>
          <button className="save" type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Create mission'}
          </button>
        </div>
      </form>
    </div>
  )
}
