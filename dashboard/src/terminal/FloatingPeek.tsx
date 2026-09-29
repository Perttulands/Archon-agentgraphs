import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { harnessIcon } from '../components/harnessIcons'
import FloatingFrameHandles from '../windows/FloatingFrameHandles'
import { useFloatingWindow } from '../windows/useFloatingWindow'
import SeatTerminal from './SeatTerminal'
import { useNotice, useSeatTerminalPool } from './seatTerminalPool'
import { fetchRunSeats, pooledSeats, seatSocketUrl, seatWaitsForYou, type RunSeats } from './seatApi'
import type { TerminalConnectionState } from './terminalSession'
import type { FrameSize } from '../windows/floatingWindowSize'
import './peek.css'

const connectionText: Record<TerminalConnectionState, string> = {
  idle: 'Connecting…', connecting: 'Connecting…', open: 'Live · type to talk to the agent',
  closed: 'Seat ended or is no longer present', dropped: 'Disconnected. Refresh seats to reconnect.',
}

/**
 * A run's seats in one floating window. Every seat the operator shows keeps its
 * terminal, connection and frame while Peek is open (the pool), and the
 * terminal holds the seat's native grid with its font fitted to the window.
 * Peek wraps the grid its font fit drew, within the size it opened at, until
 * the operator sizes it (CHROTE chrote-8eyu).
 */
export default function FloatingPeek({ windowId = 'peek', runId, initialNodeId, onClose }: {
  windowId?: string; runId: string; initialNodeId?: string; onClose: () => void
}) {
  const [projection, setProjection] = useState<RunSeats | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [selectedNode, setSelectedNode] = useState(initialNodeId || '')
  const [selectedSlot, setSelectedSlot] = useState<string | null>(null)
  const [notice, announce] = useNotice()
  const win = useFloatingWindow<HTMLElement>({ id: windowId, kind: 'peek', label: 'terminal Peek', defaultSize: { width: 760, height: 430 }, onClose })
  const closeButton = useRef<HTMLButtonElement>(null)
  const request = useRef(0)
  const selection = useRef({ node: selectedNode, slot: selectedSlot })
  selection.current = { node: selectedNode, slot: selectedSlot }

  const live = useMemo(() => pooledSeats(projection?.seats ?? []), [projection])
  const pool = useSeatTerminalPool(live, announce)
  const poolRef = useRef(pool)
  poolRef.current = pool

  // A projection only ever replaces the last one once it has arrived, so the
  // terminals of seats that are still live keep their connections. `redial` is
  // the operator asking for a new observation: a selected terminal whose
  // connection was lost dials again, at the URL the new projection gives.
  const refresh = useCallback(async (redial = false) => {
    const version = ++request.current
    setLoading(true)
    setError('')
    try {
      const result = await fetchRunSeats(runId)
      if (version !== request.current) return
      const previous = selection.current
      const node = previous.node || result.seats.find(s => s.state === 'live')?.nodeId || result.seats[0]?.nodeId || ''
      const seats = result.seats.filter(s => s.nodeId === node)
      const seat = seats.find(s => s.slotId === previous.slot)
        || seats.find(s => s.controller && s.state === 'live') || seats.find(s => s.state === 'live') || seats[0]
      setProjection(result)
      setSelectedNode(node)
      setSelectedSlot(seat?.slotId ?? null)
      const url = seat && seatSocketUrl(seat)
      if (redial && url) poolRef.current.terminals.get(url)?.redialIfDropped()
    } catch (err) {
      if (version === request.current) setError(err instanceof Error ? err.message : 'Could not read run seats')
    } finally {
      if (version === request.current) setLoading(false)
    }
  }, [runId])

  useEffect(() => {
    const trigger = document.activeElement as HTMLElement | null
    closeButton.current?.focus({ preventScroll: true })
    void refresh()
    return () => {
      request.current += 1
      trigger?.focus({ preventScroll: true })
    }
  }, [refresh])

  const seats = projection?.seats.filter(s => s.nodeId === selectedNode) || []
  const selected = seats.find(s => s.slotId === selectedSlot)
  const nodeNames = new Map(projection?.seats.map(s => [s.nodeId, s.nodeTitle]))
  if (selectedNode && !nodeNames.has(selectedNode)) nodeNames.set(selectedNode, 'Selected formation · no seats')
  const nodes = [...nodeNames.entries()]
  const url = selected ? seatSocketUrl(selected) : null
  const terminal = url ? pool.terminals.get(url) ?? null : null
  const state = url ? pool.states.get(url) ?? 'idle' : 'idle'
  const box = url ? pool.boxes.get(url) : undefined

  useEffect(() => {
    if (state === 'open') terminal?.scrollToBottom()
  }, [state, terminal])

  // Size the window by the seat, as CHROTE's Peek does (chrote-8eyu,
  // chrote-wshh), until the operator sizes it. The font is fitted to the room
  // the window was placed with, never to the window, so the window wrapping the
  // grid cannot move the font; the window then wraps the box the grid took. A
  // grid that does not fit even there scrolls. The chrome is the window less
  // the room the terminal has in it.
  const [chrome, setChrome] = useState<FrameSize | null>(null)
  const root = win.rootProps.ref
  // Read with the room in one layout, so a window mid-resize is never
  // compared with the room it had before.
  const measureChrome = useCallback((room: FrameSize) => {
    const element = root.current
    if (!element) return
    const next = { width: element.offsetWidth - room.width, height: element.offsetHeight - room.height }
    setChrome(previous => previous && previous.width === next.width && previous.height === next.height ? previous : next)
  }, [root])
  const { fitTo, sizedByOperator, rect } = win
  // Peek opens where its placement finds room beside the graph, and never
  // grows past what it was given there, so it never covers what its placement
  // keeps clear (windowPlacement.ts). CHROTE's Peek is centred over the
  // workspace at up to 90% of it instead.
  const [placed, setPlaced] = useState<FrameSize | null>(null)
  if (chrome && !placed) setPlaced({ width: rect.width, height: rect.height })
  const capsRoom = !sizedByOperator && chrome && placed ? { width: placed.width - chrome.width, height: placed.height - chrome.height } : null
  useEffect(() => {
    if (!box || !chrome || !placed || !capsRoom || box.room.width !== capsRoom.width || box.room.height !== capsRoom.height) return
    fitTo({
      width: Math.min(placed.width, Math.ceil(chrome.width + box.width)),
      height: Math.min(placed.height, Math.ceil(chrome.height + box.height)),
    })
  }, [box, capsRoom?.width, capsRoom?.height]) // eslint-disable-line react-hooks/exhaustive-deps

  return <section {...win.rootProps} className={`floating-peek${win.focused ? ' focused' : ''}`} role="dialog" aria-label="Formation terminal Peek"
    data-window-id={windowId} data-window-kind="peek">
    <header className="peek-head" {...win.moveProps}>
      <span className="peek-icon">{harnessIcon(selected?.harness)}</span>
      <span className="peek-title" tabIndex={0} aria-label="Move terminal with arrow keys"
        onKeyDown={win.onMoveKeyDown}>{selected ? `${selected.nodeTitle} / ${selected.slotLabel}` : 'Terminal Peek'}</span>
      {selected?.onCall ? <span className="peek-on-call">{seatWaitsForYou(selected) ? 'On call · waiting for you' : 'On call'}</span> : null}
      <button ref={closeButton} onClick={onClose} aria-label="Close terminal Peek">Close ×</button>
    </header>
    <div className="peek-picker">
      {nodes.length > 1 ? <select aria-label="Terminal formation" value={selectedNode} onChange={event => {
        selection.current = { node: event.target.value, slot: null }
        setSelectedNode(event.target.value)
        setSelectedSlot(null)
        void refresh()
      }}>{nodes.map(([id, title]) => <option key={id} value={id}>{title}</option>)}</select> : null}
      <button onClick={() => void refresh(true)} disabled={loading}>Refresh seats</button>
    </div>
    <nav className="peek-seats" aria-label="Run seats">
      {seats.map(seat => <button key={seat.createdSeq} aria-pressed={selectedSlot === seat.slotId}
        title={`${seat.harness} · ${seat.state}`} onClick={() => {
          selection.current = { node: seat.nodeId, slot: seat.slotId }
          setSelectedSlot(seat.slotId)
          void refresh()
        }}>
        {harnessIcon(seat.harness)}{seat.slotLabel}{seat.controller && !/controller/i.test(seat.slotLabel) ? ' · controller' : ''}
        {seat.onCall ? <span className="peek-seat-on-call" title={seatWaitsForYou(seat) ? 'On call: waiting for you' : 'On call'}>on call</span> : null}
      </button>)}
    </nav>
    {!projection && loading ? <p className="peek-message" role="status">Loading run seats…</p>
      : error ? <p className="peek-message" role="alert">{error}</p>
      : url && selected ? <SeatTerminal session={terminal} columns={selected.columns!} rows={selected.rows!} box={box} room={capsRoom} onMeasure={measureChrome} />
      : <p className="peek-message" role="status">{selected
        ? `${selected.state === 'live' ? 'Terminal unavailable' : `Seat ${selected.state}`}. ${selected.reason || ''}`
        : projection?.reason || 'No terminal seats have been created for this formation.'}</p>}
    <footer className="peek-foot"><span role="status">{url ? connectionText[state] : 'No live terminal'}</span>
      <span aria-live="polite" title="Drag to select text and copy it; hold Shift if the terminal uses mouse tracking. Resizing the window changes only the font.">{notice || 'Type to talk · select to copy'}</span></footer>
    <FloatingFrameHandles handles={win.handles} activeHandle={win.activeHandle} />
  </section>
}
