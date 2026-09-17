import { useEffect, useRef } from 'react'
import { useTheme } from '../theme/ThemeContext'
import { createTerminalSession, type ConnectionState, type TerminalSession } from './terminalSession'
import { seatSocketUrl, type RunSeat } from './seatApi'

/** A seat's live terminal: native-width lines remain readable in a narrow window. */
export default function TerminalSurface({ seat, onStateChange, focusOnOpen = false }: {
  seat: RunSeat
  onStateChange: (state: ConnectionState) => void
  /** Take keyboard focus once the seat is attached, for a window opened to talk. */
  focusOnOpen?: boolean
}) {
  const host = useRef<HTMLDivElement>(null)
  const session = useRef<TerminalSession | null>(null)
  const { theme } = useTheme()
  const latest = useRef({ theme, onStateChange, focusOnOpen })
  latest.current = { theme, onStateChange, focusOnOpen }
  const url = seatSocketUrl(seat)
  useEffect(() => {
    const element = host.current
    if (!url || !element) return
    const created = createTerminalSession({
      url, columns: seat.columns!, rows: seat.rows!, theme: latest.current.theme.terminal,
      onStateChange: state => {
        if (state === 'open') {
          created.scrollToBottom()
          if (latest.current.focusOnOpen) created.focus()
        }
        latest.current.onStateChange(state)
      },
    })
    session.current = created
    created.attach(element)
    // Resize only this viewer's row count; the seat retains its native grid.
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => created.fit())
    observer?.observe(element)
    return () => { observer?.disconnect(); created.dispose(); session.current = null }
  }, [url, seat.columns, seat.rows])
  useEffect(() => { session.current?.applyTheme(theme.terminal) }, [theme])
  return <div className="peek-terminal">
    <div className="peek-scroll-controls" aria-label="Terminal scrolling">
      <button onClick={() => session.current?.scrollLines(-10)}>Older output</button>
      <button onClick={() => session.current?.scrollToBottom()}>Latest output</button>
      <button onClick={() => session.current?.scrollToStart()}>Start of line</button>
      <button onClick={() => session.current?.scrollToEnd()}>End of line</button>
    </div>
    <div ref={host} className="terminal-surface-host" data-testid="terminal-surface" onClick={() => session.current?.focus()} />
  </div>
}
