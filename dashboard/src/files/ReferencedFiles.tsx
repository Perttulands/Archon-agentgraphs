import { useFileWindows } from './FileWindows'
import { referencedFileRequest } from './fileWindowModel'
import type { ReferencedFile } from './referencedFiles'
import './referenced.css'

// Chips for the files a node references, on its card: the first few, and a +N
// button that lists the rest. Each chip opens its file in a file window, and a
// file outside the daemon's roots opens as not readable there, with its path.

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
  if (!windows || files.length === 0) return null
  const limit = onMore && max !== undefined && files.length > max ? max : files.length
  const open = (file: ReferencedFile) => windows.open(referencedFileRequest(file.ref, file.judge ? `${file.owner} (judge)` : file.owner))
  const hidden = files.slice(limit)
  return (
    <div className={`refs${className ? ` ${className}` : ''}`} data-testid={`refs-${nodeId}`} role="group" aria-label="Referenced files">
      {files.slice(0, limit).map(file => (
        <button
          key={file.ref}
          type="button"
          className={`ref-chip${file.judge ? ' judge' : ''}`}
          title={describe(file)}
          aria-label={describe(file)}
          onPointerDown={event => event.stopPropagation()}
          onClick={event => { event.stopPropagation(); open(file) }}
        >
          <span className="ref-glyph" aria-hidden="true">{file.judge ? '⚖' : '▤'}</span>
          <span className="ref-name">{file.ref.split('/').filter(Boolean).pop() || file.ref}</span>
        </button>
      ))}
      {hidden.length ? (
        <button
          type="button"
          className="ref-chip more"
          title={hidden.map(file => file.ref).join('\n')}
          aria-label={`${hidden.length} more referenced ${hidden.length === 1 ? 'file' : 'files'}`}
          onPointerDown={event => event.stopPropagation()}
          onClick={event => {
            event.stopPropagation()
            onMore?.(hidden.map(file => ({ label: file.judge ? `${file.ref} · judge ${file.owner}` : file.ref, open: () => open(file) })), event.currentTarget.getBoundingClientRect())
          }}
        >+{hidden.length}</button>
      ) : null}
    </div>
  )
}
