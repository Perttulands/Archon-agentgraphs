/* Note threads — board and element notes are ordered entries by human and
 * agent authors. The cockpit writes as COCKPIT_NOTE_AUTHOR and may edit or
 * delete only those entries; agents' entries are replied to, never changed. */
import type { NoteEntry } from './formationsTypes'
import '../styles/formations-note-threads.css'

export const COCKPIT_NOTE_AUTHOR = 'human:ui'

export function noteAuthor(author: string): { kind: 'human' | 'agent'; name: string } {
  const separator = author.indexOf(':')
  const prefix = separator < 0 ? author : author.slice(0, separator)
  const name = separator < 0 ? '' : author.slice(separator + 1)
  if (prefix === 'agent') return { kind: 'agent', name: name || 'agent' }
  return { kind: 'human', name: !name || name === 'ui' ? 'operator' : name }
}

/** Minutes are enough to tell entries apart; the timestamp is UTC. */
export function noteTime(value: string): string {
  return value ? value.slice(0, 16).replace('T', ' ') : ''
}

export function NoteThread({ label, entries, editingEntryId, busy, onEdit, onDelete }: {
  label: string
  entries: NoteEntry[]
  editingEntryId?: string
  busy: boolean
  onEdit: (entry: NoteEntry) => void
  onDelete: (entry: NoteEntry) => void
}) {
  if (!entries.length) return null
  return (
    <ol className="note-thread" aria-label={label}>
      {entries.map(entry => {
        const author = noteAuthor(entry.author)
        const own = entry.author === COCKPIT_NOTE_AUTHOR
        const when = noteTime(entry.createdAt)
        return (
          <li key={entry.id} className={`note-entry note-entry-${author.kind}${editingEntryId === entry.id ? ' editing' : ''}`} data-testid={`note-entry-${entry.id}`}>
            <div className="note-entry-head">
              <span className={`note-author note-author-${author.kind}`} title={entry.author}>{author.name}</span>
              <span className="note-time">{when}{entry.editedAt ? ' · edited' : ''}</span>
              {own ? (
                <span className="note-entry-actions">
                  <button type="button" disabled={busy} aria-label={`Edit your note from ${when}`} onClick={() => onEdit(entry)}>edit</button>
                  <button type="button" disabled={busy} aria-label={`Delete your note from ${when}`} onClick={() => onDelete(entry)}>delete</button>
                </span>
              ) : null}
            </div>
            <div className="note-entry-text">{entry.text}</div>
          </li>
        )
      })}
    </ol>
  )
}

/** The card sticky: the latest entry with its author, and how many came before. */
export function NotePreview({ title, entries }: { title: string; entries: NoteEntry[] }) {
  const latest = entries[entries.length - 1]
  if (!latest) return null
  const author = noteAuthor(latest.author)
  return (
    <div className="note-preview" role="note" aria-label={`Notes for ${title}`}>
      <span className={`note-author note-author-${author.kind}`} title={latest.author}>{author.name}</span>
      {entries.length > 1 ? <span className="note-count">+{entries.length - 1} earlier</span> : null}
      <span className="note-preview-text">{latest.text}</span>
    </div>
  )
}
