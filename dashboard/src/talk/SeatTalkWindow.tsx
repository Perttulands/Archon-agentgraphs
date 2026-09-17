import { useCallback, useEffect, useRef, useState } from 'react'
import { harnessName } from '../components/formationsCockpitVisuals'
import { harnessIcon } from '../components/harnessIcons'
import TerminalSurface from '../terminal/TerminalSurface'
import { fetchRunSeats, seatSocketUrl, seatWaitsForYou, type RunSeat } from '../terminal/seatApi'
import type { ConnectionState } from '../terminal/terminalSession'
import FloatingWindow from '../windows/FloatingWindow'
import type { WindowRect } from '../windows/windowGeometry'
import type { TalkSeat } from './talkModel'
import '../terminal/peek.css'
import './talk.css'

const connectionText: Record<ConnectionState, string> = {
  connecting: 'Connecting…', open: 'Live · type to talk to the agent', ended: 'The seat has ended',
  disconnected: 'Disconnected. Refresh to reconnect.', unavailable: 'Terminal unavailable. Refresh to check again.',
}

/** One asked seat's terminal, opened to talk a human gate through with its agent. Typing goes to the agent. */
export default function SeatTalkWindow({ seat, anchor, focusOnOpen, onClose }: {
  seat: TalkSeat
  /** Take the keyboard once the terminal is live, so typing goes straight to the agent. */
  focusOnOpen: boolean
  anchor: () => WindowRect | null
  onClose: () => void
}) {
  const [live, setLive] = useState<RunSeat | null>(null)
  const [message, setMessage] = useState('Loading the seat…')
  const [connection, setConnection] = useState<ConnectionState>('connecting')
  const [generation, setGeneration] = useState(0)
  const request = useRef(0)

  const refresh = useCallback(async () => {
    const version = ++request.current
    setLive(null)
    setMessage('Loading the seat…')
    try {
      const found = (await fetchRunSeats(seat.runId)).seats.find(item => item.createdSeq === seat.createdSeq) || null
      if (version !== request.current) return
      setLive(found)
      setConnection('connecting')
      setGeneration(value => value + 1)
      setMessage(!found ? 'This seat is no longer in the run.' : `Seat ${found.state}. ${found.reason || ''}`.trim())
    } catch (err) {
      if (version === request.current) setMessage(err instanceof Error ? err.message : 'Could not read the run seats')
    }
  }, [seat.createdSeq, seat.runId])

  useEffect(() => {
    void refresh()
    return () => { request.current += 1 }
  }, [refresh])

  const terminal = live && seatSocketUrl(live) ? live : null
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
      {seatWaitsForYou(live ?? undefined) ? <span className="peek-on-call">On call · waiting for you</span> : null}
      <button type="button" className="talk-refresh" onClick={() => void refresh()}>Refresh</button>
    </>
  )
  return (
    <FloatingWindow id={seat.windowId} kind="peek" label={`Talk with ${seat.label}`} title={title} actions={actions}
      defaultSize={{ width: 680, height: 420 }} anchor={anchor} className="talk-window" onClose={onClose}>
      {terminal
        ? <TerminalSurface key={`${terminal.createdSeq}-${generation}`} seat={terminal} focusOnOpen={focusOnOpen} onStateChange={setConnection} />
        : <p className="peek-message" role="status">{message}</p>}
      <footer className="peek-foot">
        <span role="status">{terminal ? connectionText[connection] : 'No live terminal'}</span>
        <span>Talk it through · the agent records the decision you confirm</span>
      </footer>
    </FloatingWindow>
  )
}
