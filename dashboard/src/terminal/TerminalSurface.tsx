// Ported from CHROTE dashboard/src/components/TerminalSurface.tsx (CHROTE
// 355ace49, including chrote-te47's activation on first visible layout) under
// archon-o7p.13.1. CHROTE's useTerminalSession, which gives Peek a terminal of
// its own, is not ported: every Archon seat view takes its terminal from a seat
// terminal pool (seatTerminalPool.ts), so there is one owner of terminal life.
import { useEffect, useRef, useState } from 'react'
import type { TerminalSession } from './terminalSession'

interface TerminalSurfaceProps {
  /** The terminal to show here, from a seat terminal pool. */
  session: TerminalSession | null
  /** Off screen; an already-shown terminal keeps its connection. */
  hidden?: boolean
  /** False for an ended seat: show the last frame without dialling again. */
  connect?: boolean
}

const FIT_DEBOUNCE_MS = 100

/** The one place a terminal is put on screen. */
export default function TerminalSurface({ session, hidden = false, connect = true }: TerminalSurfaceProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [shownSession, setShownSession] = useState(hidden ? null : session)

  // First display starts the attachment. After that, visibility only controls
  // fitting: neither hiding nor showing an existing frame should redial it.
  useEffect(() => {
    if (!hidden) setShownSession(session)
  }, [session, hidden])
  const hasBeenShown = session !== null && (!hidden || shownSession === session)

  useEffect(() => {
    const host = hostRef.current
    if (!host || !session || !hasBeenShown) return
    session.attach(host, { connect })
    return () => session.detach()
  }, [session, connect, hasBeenShown])

  useEffect(() => {
    const host = hostRef.current
    if (!host || !session || hidden) return
    session.fit()
    if (typeof ResizeObserver === 'undefined') return
    let timer: ReturnType<typeof setTimeout>
    const observer = new ResizeObserver(() => {
      clearTimeout(timer)
      timer = setTimeout(() => session.fit(), FIT_DEBOUNCE_MS)
    })
    observer.observe(host)
    return () => {
      clearTimeout(timer)
      observer.disconnect()
    }
  }, [session, hidden])

  return (
    <div
      ref={hostRef}
      className="terminal-surface-host"
      data-testid="terminal-surface"
      style={hidden ? { display: 'none' } : undefined}
    />
  )
}
