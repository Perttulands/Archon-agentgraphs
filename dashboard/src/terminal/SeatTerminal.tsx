// A seat's terminal in a window: CHROTE's Peek fit (chrote-8eyu, chrote-wshh)
// around a pooled terminal. The terminal holds the seat's native grid, the
// 160x48 window the executor pins plus its status line, and fits its font to
// the room this view gives it: the largest font, no larger than 14px, at which
// every row and column fits, down to a floor of 11px. Dragging the window
// changes only the font; nothing is sent to the seat.
//
// Departure from CHROTE, and why: below the floor CHROTE clips the grid,
// anchored to the bottom left. Archon scrolls it instead, starting at the
// bottom left, with Start of line and End of line controls, because archon-a2a
// requires every column of a pinned 160-column seat to stay readable in a
// narrow Talk window.
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import TerminalSurface from './TerminalSurface'
import type { FixedGridBox, TerminalSession } from './terminalSession'
import type { FrameSize } from '../windows/floatingWindowSize'

export default function SeatTerminal({ session, columns, rows, box, connect = true, room: givenRoom = null, onMeasure }: {
  session: TerminalSession | null
  columns: number
  rows: number
  /** The pool's report of this terminal's last fit. */
  box: FixedGridBox | undefined
  connect?: boolean
  /**
   * The room to fit the font to, when the caller decides it rather than this
   * view's size: Peek fits to its caps until the operator sizes it, so the
   * window wrapping the grid cannot move the font (chrote-wshh).
   */
  room?: FrameSize | null
  /** Told this view's own size whenever it changes. */
  onMeasure?: (size: FrameSize) => void
}) {
  const roomRef = useRef<HTMLDivElement>(null)
  const [measured, setMeasured] = useState<FrameSize | null>(null)
  const room = givenRoom ?? measured
  const onMeasureRef = useRef(onMeasure)
  onMeasureRef.current = onMeasure
  useEffect(() => { if (measured) onMeasureRef.current?.(measured) }, [measured])

  // The room is this view's scroll box, which its content never sizes. Measured
  // before paint, so a new room is never drawn with the last room's font.
  useLayoutEffect(() => {
    const element = roomRef.current
    if (!element) return
    const measure = () => {
      const next = { width: element.clientWidth, height: element.clientHeight }
      setMeasured(previous => previous && previous.width === next.width && previous.height === next.height ? previous : next)
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    if (!session || !room || room.width < 1 || room.height < 1) return
    session.setFixedGrid({ cols: columns, rows, room })
  }, [session, columns, rows, room?.width, room?.height]) // eslint-disable-line react-hooks/exhaustive-deps

  // A box counts only for the grid and room it was fitted for.
  const fitted = box && room && box.cols === columns && box.rows === rows
    && box.room.width === room.width && box.room.height === room.height ? box : null
  const overflowsWidth = Boolean(fitted && fitted.width > fitted.room.width)
  const overflowsHeight = Boolean(fitted && fitted.height > fitted.room.height)

  // A grid taller than its room opens on its newest rows, where the prompt is.
  useEffect(() => {
    const element = roomRef.current
    if (element && overflowsHeight) element.scrollTop = element.scrollHeight
  }, [overflowsHeight, session])

  return <div className="seat-terminal">
    {overflowsWidth ? <div className="seat-terminal-controls" aria-label="Terminal scrolling">
      <button type="button" onClick={() => roomRef.current?.scrollTo({ left: 0 })}>Start of line</button>
      <button type="button" onClick={() => roomRef.current?.scrollTo({ left: roomRef.current.scrollWidth })}>End of line</button>
    </div> : null}
    <div ref={roomRef} className="seat-terminal-room" data-testid="seat-terminal-room" onClick={() => session?.focus()}>
      <div className={fitted ? 'seat-terminal-grid fitted' : 'seat-terminal-grid'}
        style={fitted ? { width: Math.ceil(fitted.width), height: Math.ceil(fitted.height) } : undefined}>
        <TerminalSurface session={session} connect={connect} />
      </div>
    </div>
  </div>
}
