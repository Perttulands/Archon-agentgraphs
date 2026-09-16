import { useEffect, type ReactNode } from 'react'
import FloatingFrameHandles from './FloatingFrameHandles'
import type { FloatingWindowKind, FrameSize } from './floatingWindowSize'
import { useFloatingWindow } from './useFloatingWindow'
import './floatingWindows.css'

/**
 * A floating window with the standard chrome: a title bar to drag, a close
 * button, and handles on every edge and corner. What it holds is the caller's.
 */
export default function FloatingWindow({ id, kind, title, label, defaultSize, actions, className, onClose, children }: {
  id: string
  kind: FloatingWindowKind
  title: ReactNode
  /** The window's accessible name, and what its handles and close button call it. */
  label: string
  defaultSize: FrameSize
  /** Controls drawn in the title bar before the close button. */
  actions?: ReactNode
  className?: string
  onClose: () => void
  children: ReactNode
}) {
  const win = useFloatingWindow<HTMLElement>({ id, kind, label, defaultSize, onClose })
  const { ref } = win.rootProps

  // Opening a window takes keyboard focus; closing it gives focus back.
  useEffect(() => {
    const trigger = document.activeElement as HTMLElement | null
    ref.current?.focus({ preventScroll: true })
    return () => {
      if (trigger?.isConnected) trigger.focus({ preventScroll: true })
    }
  }, [ref])

  return (
    <section
      {...win.rootProps}
      className={`fwin${win.focused ? ' focused' : ''}${className ? ` ${className}` : ''}`}
      role="dialog"
      aria-label={label}
      data-window-id={id}
      data-window-kind={kind}
    >
      <header className="fwin-head" {...win.moveProps}>
        <span className="fwin-title" tabIndex={0} aria-label={`Move ${label} with arrow keys`} onKeyDown={win.onMoveKeyDown}>{title}</span>
        {actions}
        <button type="button" className="fwin-close" aria-label={`Close ${label}`} onClick={onClose}>×</button>
      </header>
      <div className="fwin-body">{children}</div>
      <FloatingFrameHandles handles={win.handles} activeHandle={win.activeHandle} />
    </section>
  )
}
