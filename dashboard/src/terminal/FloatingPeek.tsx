import { useCallback, useEffect, useRef, useState } from 'react'
import { harnessIcon } from '../components/harnessIcons'
import FloatingFrameHandles from '../windows/FloatingFrameHandles'
import { useFloatingWindow } from '../windows/useFloatingWindow'
import TerminalSurface from './TerminalSurface'
import { fetchRunSeats, seatSocketUrl, seatWaitsForYou, type RunSeats } from './seatApi'
import type { ConnectionState } from './terminalSession'
import './peek.css'

const connectionText: Record<ConnectionState, string> = {
  connecting: 'Connecting…', open: 'Live · type to talk to the agent', ended: 'Seat ended or is no longer present',
  disconnected: 'Disconnected. Refresh seats to reconnect.', unavailable: 'Terminal unavailable. Refresh seats to check again.',
}

export default function FloatingPeek({ windowId = 'peek', runId, initialNodeId, onClose }: {
  windowId?: string; runId: string; initialNodeId?: string; onClose: () => void
}) {
  const [projection, setProjection] = useState<RunSeats | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [selectedNode, setSelectedNode] = useState(initialNodeId || '')
  const [selectedSlot, setSelectedSlot] = useState<string | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting')
  const [generation, setGeneration] = useState(0)
  const win = useFloatingWindow<HTMLElement>({ id: windowId, kind: 'peek', label: 'terminal Peek', defaultSize: { width: 760, height: 430 }, onClose })
  const closeButton = useRef<HTMLButtonElement>(null)
  const request = useRef(0)
  const selection = useRef({ node: selectedNode, slot: selectedSlot })
  selection.current = { node: selectedNode, slot: selectedSlot }

  const refresh = useCallback(async () => {
    const version = ++request.current
    setLoading(true)
    setError('')
    // Refresh is the explicit boundary for a new observation. Never reconnect
    // using a cached URL after a disconnect or a newer seat has been created.
    setProjection(null)
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
      setConnectionState('connecting')
      setGeneration(value => value + 1)
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
  const terminalAvailable = selected && seatSocketUrl(selected)
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
      <button onClick={() => void refresh()} disabled={loading}>Refresh seats</button>
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
    {loading ? <p className="peek-message" role="status">Loading run seats…</p>
      : error ? <p className="peek-message" role="alert">{error}</p>
      : terminalAvailable && selected ? <TerminalSurface key={`${selected.createdSeq}-${generation}`} seat={selected} onStateChange={setConnectionState} />
      : <p className="peek-message" role="status">{selected
        ? `${selected.state === 'live' ? 'Terminal unavailable' : `Seat ${selected.state}`}. ${selected.reason || ''}`
        : projection?.reason || 'No terminal seats have been created for this formation.'}</p>}
    <footer className="peek-foot"><span role="status">{terminalAvailable ? connectionText[connectionState] : 'No live terminal'}</span>
      <span title="Drag to select text; hold Shift if the terminal uses mouse tracking. Resize the window to resize the terminal.">Type to talk · select to copy</span></footer>
    <FloatingFrameHandles handles={win.handles} activeHandle={win.activeHandle} />
  </section>
}
