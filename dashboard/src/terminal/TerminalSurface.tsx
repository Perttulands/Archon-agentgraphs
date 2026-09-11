import { useEffect, useRef } from 'react'
import { useTheme } from '../theme/ThemeContext'
import { createTerminalSession, type ConnectionState, type TerminalSession } from './terminalSession'
import { seatSocketUrl, type RunSeat } from './seatApi'

export default function TerminalSurface({ seat, onStateChange }: {
  seat: RunSeat; onStateChange: (state: ConnectionState) => void
}) {
  const host = useRef<HTMLDivElement>(null)
  const session = useRef<TerminalSession | null>(null)
  const { theme } = useTheme()
  const latest = useRef({ theme, onStateChange })
  latest.current = { theme, onStateChange }
  const url = seatSocketUrl(seat)
  useEffect(() => {
    if (!url || !host.current) return
    const created = createTerminalSession({
      url, columns: seat.columns!, rows: seat.rows!, theme: latest.current.theme.terminal,
      onStateChange: state => latest.current.onStateChange(state),
    })
    session.current = created
    created.attach(host.current)
    return () => { created.dispose(); session.current = null }
  }, [url, seat.columns, seat.rows])
  useEffect(() => { session.current?.applyTheme(theme.terminal) }, [theme])
  return <div className="peek-terminal">
    <div className="peek-scroll-controls" aria-label="Terminal scrolling">
      <button onClick={() => session.current?.scrollLines(-10)}>Older output</button>
      <button onClick={() => session.current?.scrollToBottom()}>Latest output</button>
    </div>
    <div ref={host} className="terminal-surface-host" data-testid="terminal-surface" />
  </div>
}
