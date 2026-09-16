/* A node's or the board's note thread in a floating window: the whole thread,
 * a reply box, and edit or delete of the operator's own entries. The draft
 * lives with the cockpit, so closing the window keeps it. */
import { useEffect, useRef } from 'react'
import FloatingWindow from '../windows/FloatingWindow'
import type { NoteEntry } from './formationsTypes'
import { NoteThread } from './NoteThread'

export const BOARD_NOTE_TARGET = 'board'

export function noteWindowId(target: string): string {
  return `note:${target}`
}

export default function NoteWindow({ target, title, entries, draft, editingEntryId, saving, error, conflict, onDraft, onSave, onCancelEdit, onEdit, onDelete, onReload, onClose }: {
  target: string
  title: string
  entries: NoteEntry[]
  draft: string
  editingEntryId?: string
  saving: boolean
  error: string
  conflict: boolean
  onDraft: (text: string) => void
  onSave: () => void
  onCancelEdit: () => void
  onEdit: (entry: NoteEntry) => void
  onDelete: (entry: NoteEntry) => void
  onReload: () => void
  onClose: () => void
}) {
  const board = target === BOARD_NOTE_TARGET
  const subject = board ? 'the board' : title
  const reply = useRef<HTMLTextAreaElement>(null)
  // A new window is ready to write in; it opens after the window takes focus.
  useEffect(() => {
    reply.current?.focus({ preventScroll: true })
  }, [])
  const editing = Boolean(editingEntryId)
  return (
    <FloatingWindow
      id={noteWindowId(target)}
      kind="note"
      title={board ? 'Board notes' : `Notes · ${title}`}
      label={board ? 'board notes' : `notes for ${title}`}
      defaultSize={{ width: 400, height: 440 }}
      className="note-window"
      onClose={onClose}
    >
      <div className="note-window-body">
        {entries.length ? (
          <NoteThread
            label={board ? 'Board note thread' : `Note thread for ${title}`}
            entries={entries}
            editingEntryId={editingEntryId}
            busy={saving}
            onEdit={onEdit}
            onDelete={onDelete}
          />
        ) : <p className="note-empty">No notes yet. Write the idea for {subject} below.</p>}
        <textarea
          ref={reply}
          className="note-reply"
          aria-label={`Note for ${subject}`}
          value={draft}
          placeholder={editing ? 'Edit your note…' : entries.length ? 'Reply…' : 'Vision, constraints, or what this step should do…'}
          onChange={event => onDraft(event.target.value)}
        />
        <div className="note-reply-actions">
          {editing ? <button type="button" className="cancel" disabled={saving} onClick={onCancelEdit}>Cancel edit</button> : null}
          <button type="button" disabled={!draft.trim() || saving} onClick={onSave}>
            {saving ? 'Saving…' : editing ? 'Save edit' : entries.length ? 'Reply' : 'Add note'}
          </button>
        </div>
        {error ? <div className="note-error" role="alert">{error}</div> : null}
        {conflict ? (
          <button type="button" className="note-reload" disabled={saving} onClick={onReload}>Reload shared notes (discard local draft)</button>
        ) : null}
      </div>
    </FloatingWindow>
  )
}
