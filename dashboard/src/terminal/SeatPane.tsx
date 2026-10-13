// A step's seats inside its node window: the terminal of the seat shown, and
// one tab per seat when the step has several. The terminal holds the seat's
// native grid and fits its font to the room the window leaves it, as Talk
// does (SeatTalkWindow.tsx); every seat shown keeps its connection while the
// window is open (the pool).
import { useEffect, useMemo, useState } from 'react'
import { harnessIcon } from '../components/harnessIcons'
import SeatTerminal from './SeatTerminal'
import { useNotice, useSeatTerminalPool } from './seatTerminalPool'
import { fetchRunSeats, pooledSeats, seatSocketUrl, seatWaitsForYou, type RunSeat } from './seatApi'
import './peek.css'

const SEATS_POLL_MS = 5000

/** The seats a run made for one step, read again while the window is open; null until first read. */
export function useNodeSeats(runId: string | undefined, nodeId: string): RunSeat[] | null {
  const [read, setRead] = useState<{ key: string; seats: RunSeat[] } | null>(null)
  const key = `${runId || ''}/${nodeId}`
  useEffect(() => {
    if (!runId) return
    let current = true
    const load = () => fetchRunSeats(runId).then(
      result => { if (current) setRead({ key, seats: (result.seats || []).filter(seat => seat.nodeId === nodeId) }) },
      () => { if (current) setRead(previous => previous?.key === key ? previous : { key, seats: [] }) },
    )
    void load()
    const timer = window.setInterval(load, SEATS_POLL_MS)
    return () => { current = false; window.clearInterval(timer) }
  }, [runId, nodeId, key])
  return runId && read?.key === key ? read.seats : null
}

/** Whether any of the seats has a terminal to show. */
export function hasLiveSeat(seats: readonly RunSeat[] | null): boolean {
  return Boolean(seats?.some(seat => seatSocketUrl(seat)))
}

export default function SeatPane({ seats }: { seats: RunSeat[] }) {
  const [chosen, setChosen] = useState<string | null>(null)
  const [notice, announce] = useNotice()
  // Rebuilt only when a seat's terminal changes, so a poll never redials one.
  const liveKey = seats.map(seat => `${seat.createdSeq}:${seatSocketUrl(seat) || ''}:${seat.columns}x${seat.rows}`).join('|')
  const live = useMemo(() => pooledSeats(seats), [liveKey]) // eslint-disable-line react-hooks/exhaustive-deps
  const pool = useSeatTerminalPool(live, announce)
  const selected = seats.find(seat => seat.slotId === chosen && seatSocketUrl(seat))
    || seats.find(seat => seat.controller && seatSocketUrl(seat)) || seats.find(seat => seatSocketUrl(seat)) || seats[0]
  const url = selected ? seatSocketUrl(selected) : null
  const terminal = url ? pool.terminals.get(url) ?? null : null
  const state = url ? pool.states.get(url) ?? 'idle' : 'idle'

  useEffect(() => {
    if (state === 'open') terminal?.scrollToBottom()
  }, [state, terminal])

  return (
    <div className="seat-pane" data-testid="seat-pane">
      {seats.length > 1 ? (
        <nav className="peek-seats" aria-label="Seats">
          {seats.map(seat => (
            <button type="button" key={seat.createdSeq} aria-pressed={selected?.slotId === seat.slotId} title={`${seat.sessionName} · ${seat.state}`}
              onClick={() => setChosen(seat.slotId)}>
              {harnessIcon(seat.harness)}{seat.slotLabel}{seat.controller && !/controller/i.test(seat.slotLabel) ? ' · controller' : ''}
              {seat.onCall ? <span className="peek-seat-on-call">{seatWaitsForYou(seat) ? 'waiting for you' : 'on call'}</span> : null}
            </button>
          ))}
        </nav>
      ) : null}
      {url && selected
        ? <SeatTerminal session={terminal} columns={selected.columns!} rows={selected.rows!} box={pool.boxes.get(url)} />
        : <p className="peek-message" role="status">{selected ? `Seat ${selected.state}. ${selected.reason || ''}` : 'No seat'}</p>}
      {state === 'dropped' || state === 'closed' || notice ? (
        <footer className="peek-foot">
          <span role="status">{state === 'dropped' ? 'Disconnected' : state === 'closed' ? 'Seat ended' : ''}</span>
          {state === 'dropped' ? <button type="button" onClick={() => terminal?.redialIfDropped()}>Reconnect</button> : <span aria-live="polite">{notice}</span>}
        </footer>
      ) : null}
    </div>
  )
}
