import { useEffect, useRef, type PointerEvent as ReactPointerEvent } from 'react'
import LinkedText from '../files/LinkedText'
/* Notes on the canvas, as on a whiteboard. Every note thread is a sticky in
 * world space: it pans and zooms with the cards, and opening it expands it
 * in place to read the whole thread and write. A sticky sits under its card
 * until it is dragged; then it stays where it was dropped. The mission's own
 * thread is the notepad by the Input card. */
import type { NoteEntry } from './formationsTypes'
import { NoteThread, noteAuthor } from './NoteThread'
import { BOARD_NOTE_TARGET } from './NoteWindow'

/** A card's box in world coordinates. */
export interface NoteAnchor {
  x: number
  y: number
  width: number
  height: number
}

/** The cards a note thread can belong to. */
export const NOTE_CARDS = '.formation[data-node],.gatecard[data-node],.missioncard[data-node],.toolcard[data-node],.endcard[data-node],.limitcard[data-node]'

/** Where a dragged sticky is kept in the mission layout. */
export function noteLayoutId(target: string): string {
  return `note:${target}`
}

export const STICKY_WIDTH = 240
export const OPEN_STICKY_WIDTH = 320
const STICKY_GAP = 6

/** A sticky's home under its card, right-aligned with it. */
export function stickyHome(anchor: NoteAnchor, below = 0): { x: number; y: number } {
  return { x: Math.max(anchor.x, anchor.x + anchor.width - STICKY_WIDTH), y: anchor.y + anchor.height + STICKY_GAP + below }
}

export function sameNoteAnchors(a: ReadonlyMap<string, NoteAnchor>, b: ReadonlyMap<string, NoteAnchor>): boolean {
  if (a.size !== b.size) return false
  for (const [id, anchor] of a) {
    const other = b.get(id)
    if (!other || other.x !== anchor.x || other.y !== anchor.y || other.width !== anchor.width || other.height !== anchor.height) return false
  }
  return true
}

function AuthorChip({ author }: { author: string }) {
  const { kind, name } = noteAuthor(author)
  return <span className={`note-author note-author-${kind}`} title={author}>{name}</span>
}

export interface CanvasNote {
  target: string
  title: string
  entries: NoteEntry[]
  x: number
  y: number
  open: boolean
  draft: string
  editingEntryId?: string
  error: string
}

export interface CanvasNoteActions {
  /** A press on the sticky: a drag moves it, a click opens it. */
  onGrab: (event: ReactPointerEvent<HTMLElement>, target: string) => void
  onOpen: (target: string) => void
  onClose: (target: string) => void
  onDraft: (target: string, text: string) => void
  onSave: (target: string) => void
  onCancelEdit: (target: string) => void
  onEdit: (target: string, entry: NoteEntry) => void
  onDelete: (target: string, entry: NoteEntry) => void
  onReload: () => void
}

// Presses on these act on the sticky's controls, not on the sticky.
const CONTROLS = 'button,a,input,textarea,select,.note-scroll'

function OpenSticky({ note, readOnly, saving, conflict, actions }: { note: CanvasNote; readOnly: boolean; saving: boolean; conflict: boolean; actions: CanvasNoteActions }) {
  const mission = note.target === BOARD_NOTE_TARGET
  const subject = mission ? 'the mission' : note.title
  const label = mission ? 'mission notes' : `notes for ${note.title}`
  const reply = useRef<HTMLTextAreaElement>(null)
  // An opened note is ready to write in.
  useEffect(() => {
    reply.current?.focus({ preventScroll: true })
  }, [])
  const editing = Boolean(note.editingEntryId)
  const latest = note.entries[note.entries.length - 1]
  const kind = latest ? noteAuthor(latest.author).kind : 'human'
  return (
    <section
      role="dialog"
      aria-label={label}
      className={`note-sticky note-sticky-${kind} open${mission ? ' note-sticky-mission' : ''}`}
      data-note-node={note.target}
      style={{ left: note.x, top: note.y, width: OPEN_STICKY_WIDTH }}
      onPointerDown={event => {
        event.stopPropagation()
        if (!(event.target as HTMLElement).closest(CONTROLS)) actions.onGrab(event, note.target)
      }}
      onKeyDown={event => {
        if (event.key !== 'Escape') return
        event.stopPropagation()
        actions.onClose(note.target)
      }}
    >
      <div className="note-sticky-head">
        <span className="note-sticky-title">{mission ? 'Mission notes' : note.title}</span>
        <button type="button" className="note-sticky-close" aria-label={`Close ${label}`} onClick={() => actions.onClose(note.target)}>×</button>
      </div>
      {note.entries.length ? (
        <div className="note-scroll">
          <NoteThread
            label={mission ? 'Mission note thread' : `Note thread for ${note.title}`}
            entries={note.entries}
            editingEntryId={note.editingEntryId}
            busy={saving || readOnly}
            onEdit={entry => actions.onEdit(note.target, entry)}
            onDelete={entry => actions.onDelete(note.target, entry)}
          />
        </div>
      ) : null}
      <textarea
        ref={reply}
        className="note-reply"
        aria-label={`Note for ${subject}`}
        value={note.draft}
        readOnly={readOnly}
        placeholder={editing ? 'Edit your note…' : note.entries.length ? 'Reply…' : mission ? 'What this mission is for…' : 'What this step should do…'}
        onChange={event => actions.onDraft(note.target, event.target.value)}
        onKeyDown={event => {
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && note.draft.trim() && !saving && !readOnly) {
            event.preventDefault()
            actions.onSave(note.target)
          }
        }}
      />
      <div className="note-reply-actions">
        {editing ? <button type="button" className="cancel" disabled={saving} onClick={() => actions.onCancelEdit(note.target)}>Cancel edit</button> : null}
        <button type="button" disabled={readOnly || !note.draft.trim() || saving} onClick={() => actions.onSave(note.target)}>
          {saving ? 'Saving…' : editing ? 'Save edit' : note.entries.length ? 'Reply' : 'Add note'}
        </button>
      </div>
      {note.error ? <div className="note-error" role="alert">{note.error}</div> : null}
      {conflict ? (
        <button type="button" className="note-reload" disabled={saving} onClick={actions.onReload}>Reload shared notes (discard local draft)</button>
      ) : null}
    </section>
  )
}

function ClosedSticky({ note, actions }: { note: CanvasNote; actions: CanvasNoteActions }) {
  const mission = note.target === BOARD_NOTE_TARGET
  const latest = note.entries[note.entries.length - 1]
  const kind = latest ? noteAuthor(latest.author).kind : 'human'
  return (
    <section
      role="note"
      aria-label={mission ? 'Mission notes' : `Notes for ${note.title}`}
      className={`note-sticky note-sticky-${kind}${mission ? ' note-sticky-mission' : ''}${latest ? '' : ' empty'}`}
      data-note-node={note.target}
      style={{ left: note.x, top: note.y, width: STICKY_WIDTH }}
      onPointerDown={event => {
        event.stopPropagation()
        if (!(event.target as HTMLElement).closest(CONTROLS)) actions.onGrab(event, note.target)
      }}
    >
      <div className="note-sticky-head">
        {mission ? <span className="note-sticky-title">Mission notes</span> : latest ? <AuthorChip author={latest.author} /> : null}
        {note.entries.length > 1 ? <span className="note-count">+{note.entries.length - 1} earlier</span> : null}
        <button
          type="button"
          className="note-sticky-open"
          aria-label={mission ? 'Mission notes' : `Open the note thread for ${note.title}`}
          onClick={event => { event.stopPropagation(); actions.onOpen(note.target) }}
        >open</button>
      </div>
      <div className="note-sticky-text">{latest ? <LinkedText text={latest.text} /> : 'Write what this mission is for.'}</div>
    </section>
  )
}

export function NoteLayer({ notes, readOnly, saving, conflict, actions }: {
  notes: readonly CanvasNote[]
  readOnly: boolean
  saving: boolean
  conflict: boolean
  actions: CanvasNoteActions
}) {
  return (
    <div className="note-layer" data-testid="note-layer">
      {notes.map(note => note.open
        ? <OpenSticky key={note.target} note={note} readOnly={readOnly} saving={saving} conflict={conflict} actions={actions} />
        : <ClosedSticky key={note.target} note={note} actions={actions} />)}
    </div>
  )
}
