/* Inline title editor for a canvas node header. Enter or leaving the field
 * saves the trimmed title (an empty one clears it); Escape cancels. */
import { useRef, useState } from 'react'
import '../styles/formations-node-editing.css'

export function InlineTitleEditor({ value, label, className, onCommit, onCancel }: {
  value: string
  label: string
  /** The title element's class, so the field keeps its look. */
  className: string
  onCommit: (title: string) => void
  onCancel: () => void
}) {
  const [draft, setDraft] = useState(value)
  const finished = useRef(false)
  const finish = (commit: boolean) => {
    if (finished.current) return
    finished.current = true
    if (commit) onCommit(draft.trim())
    else onCancel()
  }
  return (
    <input
      className={`${className} inline-title`}
      aria-label={label}
      autoFocus
      value={draft}
      spellCheck={false}
      onFocus={event => event.currentTarget.select()}
      onChange={event => setDraft(event.target.value)}
      onKeyDown={event => {
        if (event.key === 'Enter') {
          event.preventDefault()
          finish(true)
        } else if (event.key === 'Escape') {
          event.preventDefault()
          event.stopPropagation()
          finish(false)
        }
      }}
      onBlur={() => finish(true)}
      onPointerDown={event => event.stopPropagation()}
      onDoubleClick={event => event.stopPropagation()}
      onContextMenu={event => event.stopPropagation()}
    />
  )
}
