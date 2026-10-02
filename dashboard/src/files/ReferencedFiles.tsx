import { fileAnchor, useFileWindows } from './FileWindows'
import { referencedFileRequest } from './fileWindowModel'
import { useFileProblems, type ReferencedFile } from './referencedFiles'
import './referenced.css'

// Chips for the files a node references, on its card: the first few, and a +N
// button that lists the rest. Each chip opens its file in a file window; a file
// the daemon cannot read opens with the daemon's reason and its path. A chip
// whose file does not exist, or whose path is relative, is flagged.

export interface HiddenReferencedFile {
  label: string
  open: () => void
}

function describe(file: ReferencedFile): string {
  return file.judge ? `Open ${file.ref}, from the brief of judge ${file.owner}` : `Open ${file.ref}`
}

export function ReferencedFiles({ nodeId, files, max, onMore, className }: {
  nodeId: string
  files: readonly ReferencedFile[]
  /** How many chips show before a +N button; needs onMore to list the rest. */
  max?: number
  onMore?: (hidden: HiddenReferencedFile[], anchor: DOMRect) => void
  className?: string
}) {
  const windows = useFileWindows()
  const problems = useFileProblems()
  if (!windows || files.length === 0) return null
  const limit = onMore && max !== undefined && files.length > max ? max : files.length
  const open = (file: ReferencedFile, control: Element) => windows.open(referencedFileRequest(file.ref, file.judge ? `${file.owner} (judge)` : file.owner), fileAnchor(control))
  const hidden = files.slice(limit)
  const label = (file: ReferencedFile) => {
    const problem = problems.get(file.ref)
    return problem ? `${describe(file)}, which ${problem}` : describe(file)
  }
  return (
    <div className={`refs${className ? ` ${className}` : ''}`} data-testid={`refs-${nodeId}`} role="group" aria-label="Referenced files">
      {files.slice(0, limit).map(file => (
        <button
          key={file.ref}
          type="button"
          className={`ref-chip${file.judge ? ' judge' : ''}${problems.has(file.ref) ? ' missing' : ''}`}
          title={label(file)}
          aria-label={label(file)}
          onPointerDown={event => event.stopPropagation()}
          onClick={event => { event.stopPropagation(); open(file, event.currentTarget) }}
        >
          <span className="ref-glyph" aria-hidden="true">{problems.has(file.ref) ? '!' : file.judge ? '⚖' : '▤'}</span>
          <span className="ref-name">{file.ref.split('/').filter(Boolean).pop() || file.ref}</span>
        </button>
      ))}
      {hidden.length ? (
        <button
          type="button"
          className={`ref-chip more${hidden.some(file => problems.has(file.ref)) ? ' missing' : ''}`}
          title={hidden.map(file => (problems.has(file.ref) ? `${file.ref} ${problems.get(file.ref)}` : file.ref)).join('\n')}
          aria-label={`${hidden.length} more referenced ${hidden.length === 1 ? 'file' : 'files'}`}
          onPointerDown={event => event.stopPropagation()}
          onClick={event => {
            event.stopPropagation()
            const more = event.currentTarget
            onMore?.(hidden.map(file => {
              const shown = file.judge ? `${file.ref} · judge ${file.owner}` : file.ref
              const problem = problems.get(file.ref)
              return { label: problem ? `${shown} · ${problem}` : shown, open: () => open(file, more) }
            }), more.getBoundingClientRect())
          }}
        >+{hidden.length}</button>
      ) : null}
    </div>
  )
}
