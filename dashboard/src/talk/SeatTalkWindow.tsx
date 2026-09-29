import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { harnessName } from '../components/formationsCockpitVisuals'
import { harnessIcon } from '../components/harnessIcons'
import SeatTerminal from '../terminal/SeatTerminal'
import { useNotice, useSeatTerminalPool } from '../terminal/seatTerminalPool'
import { fetchRunSeats, pooledSeats, seatSocketUrl, type RunSeat } from '../terminal/seatApi'
import type { TerminalConnectionState } from '../terminal/terminalSession'
import FloatingWindow from '../windows/FloatingWindow'
import type { WindowRect } from '../windows/windowGeometry'
import type { TalkSeat } from './talkModel'
import '../terminal/peek.css'
import './talk.css'

const connectionText: Record<TerminalConnectionState, string> = {
  idle: 'Connecting…', connecting: 'Connecting…', open: 'Live · type to talk to the agent', closed: 'The seat has ended',
  dropped: 'Disconnected. Refresh to reconnect.',
}

/** One asked seat's terminal, opened to talk a human gate through with its agent. Typing goes to the agent. */
export default function SeatTalkWindow({ seat, status, anchor, focusOnOpen, onClose }: {
  seat: TalkSeat
  /** From the run projection, so it follows the decision while the window is open. */
  status: 'waiting' | 'decided' | null
  /** Take the keyboard once the terminal is live, so typing goes straight to the agent. */
  focusOnOpen: boolean
  anchor: () => WindowRect | null
  onClose: () => void
}) {
  const [live, setLive] = useState<RunSeat | null>(null)
  const [message, setMessage] = useState('Loading the seat…')
  const [notice, announce] = useNotice()
  const request = useRef(0)
  const pool = useSeatTerminalPool(useMemo(() => pooledSeats(live ? [live] : []), [live]), announce)
  const poolRef = useRef(pool)
  poolRef.current = pool

  // Refresh keeps the terminal of a seat that is still live, and dials again
  // one whose connection was lost.
  const refresh = useCallback(async (redial = false) => {
    const version = ++request.current
    try {
      const found = (await fetchRunSeats(seat.runId)).seats.find(item => item.createdSeq === seat.createdSeq) || null
      if (version !== request.current) return
      setLive(found)
      setMessage(!found ? 'This seat is no longer in the run.' : `Seat ${found.state}. ${found.reason || ''}`.trim())
      const url = found && seatSocketUrl(found)
      if (redial && url) poolRef.current.terminals.get(url)?.redialIfDropped()
    } catch (err) {
      if (version === request.current) setMessage(err instanceof Error ? err.message : 'Could not read the run seats')
    }
  }, [seat.createdSeq, seat.runId])

  useEffect(() => {
    void refresh()
    return () => { request.current += 1 }
  }, [refresh])

  const url = live ? seatSocketUrl(live) : null
  const terminal = url ? pool.terminals.get(url) ?? null : null
  const connection = url ? pool.states.get(url) ?? 'idle' : 'idle'
  useEffect(() => {
    if (connection !== 'open' || !terminal) return
    terminal.scrollToBottom()
    if (focusOnOpen) terminal.focus()
  }, [connection, terminal]) // eslint-disable-line react-hooks/exhaustive-deps
  // The seat has closed once its terminal ends, or the run no longer shows it live.
  const closed = connection === 'closed' || Boolean(live && live.state !== 'live')
  const title = (
    <span className="talk-title">
      <span className="talk-icon">{harnessIcon(seat.harness)}</span>
      <span className="talk-agent">{seat.agent}</span>
      {seat.harness ? <span className="talk-harness">{harnessName(seat.harness)}</span> : null}
      <span className="talk-where">{seat.formationTitle} / {seat.slotLabel}</span>
    </span>
  )
  const actions = (
    <>
      {status === 'waiting' ? <span className="peek-on-call">On call · waiting for you</span> : null}
      <button type="button" className="talk-refresh" onClick={() => void refresh(true)}>Refresh</button>
    </>
  )
  return (
    <FloatingWindow id={seat.windowId} kind="peek" label={`Talk with ${seat.label}`} title={title} actions={actions}
      defaultSize={{ width: 680, height: 420 }} anchor={anchor} className="talk-window" onClose={onClose}>
      {status === 'decided'
        ? <p className="talk-decided" role="status">{closed ? 'Decision recorded; this agent has closed.' : 'Decision recorded; this agent closes when idle.'}</p>
        : null}
      {url && live
        ? <SeatTerminal session={terminal} columns={live.columns!} rows={live.rows!} box={pool.boxes.get(url)} />
        : <p className="peek-message" role="status">{message}</p>}
      <footer className="peek-foot">
        <span role="status">{url ? connectionText[connection] : 'No live terminal'}</span>
        <span aria-live="polite">{notice || (status === 'decided' ? 'Type to talk · select to copy' : 'Talk it through · the agent records the decision you confirm')}</span>
      </footer>
    </FloatingWindow>
  )
}
