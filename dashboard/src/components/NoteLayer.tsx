/* Notes on the canvas. Each noted node gets a sticky in a layer above the
 * cards, so no card covers it. Preview shows the latest entry and who wrote
 * it; full shows the whole thread. A sticky opens the node's note window. */
import type { WindowRect } from '../windows/windowGeometry'
import type { NoteEntry } from './formationsTypes'
import { noteAuthor, noteTime } from './NoteThread'

export type NotesMode = 'hidden' | 'preview' | 'full'
export const NOTES_MODES: readonly NotesMode[] = ['hidden', 'preview', 'full']
export const NOTES_MODE_KEY = 'chrote-formations-notes-mode'

export function readNotesMode(): NotesMode {
  try {
    const stored = window.localStorage.getItem(NOTES_MODE_KEY)
    return NOTES_MODES.includes(stored as NotesMode) ? stored as NotesMode : 'preview'
  } catch {
    return 'preview'
  }
}

export function writeNotesMode(mode: NotesMode) {
  try {
    window.localStorage.setItem(NOTES_MODE_KEY, mode)
  } catch {
    // Storage may be unavailable; the switch still works for this page.
  }
}

/** A card's box in world coordinates. */
export interface NoteAnchor {
  x: number
  y: number
  width: number
  height: number
}

export interface NodeNotes {
  nodeId: string
  title: string
  entries: NoteEntry[]
}

/** The cards a note thread can belong to. */
export const NOTE_CARDS = '.formation[data-node],.gatecard[data-node],.missioncard[data-node],.toolcard[data-node]'

/**
 * Where a node's idea sits on screen, in viewport pixels: its card and the
 * sticky under it. The node's note window opens beside this. Null when the
 * card is not drawn.
 */
export function noteWindowAnchor(world: HTMLElement | null, nodeId: string): WindowRect | null {
  if (!world) return null
  const boxes = [...world.querySelectorAll<HTMLElement>(`${NOTE_CARDS},.note-sticky[data-note-node]`)]
    .filter(element => (element.dataset.node ?? element.dataset.noteNode) === nodeId)
    .map(element => element.getBoundingClientRect())
    .filter(box => box.width > 0 && box.height > 0)
  if (!boxes.length) return null
  const left = Math.min(...boxes.map(box => box.left))
  const top = Math.min(...boxes.map(box => box.top))
  const right = Math.max(...boxes.map(box => box.right))
  const bottom = Math.max(...boxes.map(box => box.bottom))
  return { left, top, width: right - left, height: bottom - top }
}

const STICKY_WIDTH: Record<Exclude<NotesMode, 'hidden'>, number> = { preview: 240, full: 300 }
const STICKY_GAP = 6

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

export function NoteLayer({ mode, anchors, notes, onOpen }: {
  mode: NotesMode
  anchors: ReadonlyMap<string, NoteAnchor>
  notes: readonly NodeNotes[]
  onOpen: (nodeId: string) => void
}) {
  if (mode === 'hidden') return null
  const width = STICKY_WIDTH[mode]
  return (
    <div className={`note-layer ${mode}`} data-testid="note-layer">
      {notes.map(({ nodeId, title, entries }) => {
        const anchor = anchors.get(nodeId)
        const latest = entries[entries.length - 1]
        if (!anchor || !latest) return null
        const left = Math.max(anchor.x, anchor.x + anchor.width - width)
        return (
          <section
            key={nodeId}
            role="note"
            aria-label={`Notes for ${title}`}
            className={`note-sticky note-sticky-${noteAuthor(latest.author).kind}`}
            data-note-node={nodeId}
            style={{ left, top: anchor.y + anchor.height + STICKY_GAP, width }}
            onPointerDown={event => event.stopPropagation()}
            onWheel={mode === 'full' ? event => event.stopPropagation() : undefined}
            onClick={mode === 'preview' ? () => onOpen(nodeId) : undefined}
          >
            <div className="note-sticky-head">
              {mode === 'preview' ? <AuthorChip author={latest.author} /> : <span className="note-sticky-title">{entries.length === 1 ? '1 note' : `${entries.length} notes`}</span>}
              {mode === 'preview' && entries.length > 1 ? <span className="note-count">+{entries.length - 1} earlier</span> : null}
              <button
                type="button"
                className="note-sticky-open"
                aria-label={`Open the note thread for ${title}`}
                onClick={event => { event.stopPropagation(); onOpen(nodeId) }}
              >open</button>
            </div>
            {mode === 'preview' ? (
              <div className="note-sticky-text">{latest.text}</div>
            ) : (
              <ol className="note-sticky-thread">
                {entries.map(entry => (
                  <li key={entry.id} className={`note-sticky-entry note-entry-${noteAuthor(entry.author).kind}`}>
                    <div className="note-sticky-entry-head"><AuthorChip author={entry.author} /><span className="note-time">{noteTime(entry.createdAt)}</span></div>
                    <div className="note-sticky-text">{entry.text}</div>
                  </li>
                ))}
              </ol>
            )}
          </section>
        )
      })}
    </div>
  )
}
