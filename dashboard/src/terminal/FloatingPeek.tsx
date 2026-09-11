import { useCallback, useEffect, useRef, useState, type PointerEvent } from 'react'
import { harnessIcon } from '../components/harnessIcons'
import TerminalSurface from './TerminalSurface'
import { fetchRunSeats, seatSocketUrl, type RunSeats } from './seatApi'
import type { ConnectionState } from './terminalSession'
import './peek.css'

const connectionText: Record<ConnectionState, string> = {
  connecting: 'Connecting…', open: 'Live output', ended: 'Seat ended or is no longer present',
  disconnected: 'Disconnected. Refresh seats to reconnect.', unavailable: 'Observation unavailable. Refresh seats to check again.',
}

export default function FloatingPeek({ runId, initialNodeId, onClose }: {
  runId: string; initialNodeId?: string; onClose: () => void
}) {
  const [projection, setProjection] = useState<RunSeats | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [selectedNode, setSelectedNode] = useState(initialNodeId || '')
  const [selectedSlot, setSelectedSlot] = useState<string | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting')
  const [generation, setGeneration] = useState(0)
  const [position, setPosition] = useState<{ x: number; y: number } | null>(null)
  const panel = useRef<HTMLElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  const request = useRef(0)
  const drag = useRef<{ pointer: number; x: number; y: number; left: number; top: number } | null>(null)
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

  const move = useCallback((x: number, y: number) => {
    const rect = panel.current?.getBoundingClientRect()
    if (!rect || window.innerWidth <= 800) { setPosition(null); return }
    setPosition({ x: Math.max(8, Math.min(window.innerWidth - rect.width - 8, x)),
      y: Math.max(44, Math.min(window.innerHeight - rect.height - 8, y)) })
  }, [])
  useEffect(() => {
    const resize = () => {
      const rect = panel.current?.getBoundingClientRect()
      if (rect) move(rect.x, rect.y)
    }
    window.addEventListener('resize', resize)
    return () => window.removeEventListener('resize', resize)
  }, [move])

  const beginDrag = (event: PointerEvent<HTMLElement>) => {
    if (event.button !== 0 || window.innerWidth <= 800 || (event.target as HTMLElement).closest('button,select')) return
    const rect = panel.current!.getBoundingClientRect()
    drag.current = { pointer: event.pointerId, x: event.clientX, y: event.clientY, left: rect.left, top: rect.top }
    event.currentTarget.setPointerCapture(event.pointerId)
  }
  const seats = projection?.seats.filter(s => s.nodeId === selectedNode) || []
  const selected = seats.find(s => s.slotId === selectedSlot)
  const nodeNames = new Map(projection?.seats.map(s => [s.nodeId, s.nodeTitle]))
  if (selectedNode && !nodeNames.has(selectedNode)) nodeNames.set(selectedNode, 'Selected formation · no seats')
  const nodes = [...nodeNames.entries()]
  const terminalAvailable = selected && seatSocketUrl(selected)
  return <section ref={panel} className="floating-peek" role="dialog" aria-label="Formation terminal Peek"
    style={position ? { left: position.x, top: position.y } : undefined}
    onKeyDown={event => { event.stopPropagation(); if (event.key === 'Escape') onClose() }}
    onPointerDown={event => event.stopPropagation()}>
    <header className="peek-head" onPointerDown={beginDrag}
      onPointerMove={event => {
        const current = drag.current
        if (current?.pointer === event.pointerId) move(current.left + event.clientX - current.x, current.top + event.clientY - current.y)
      }} onPointerUp={() => { drag.current = null }} onPointerCancel={() => { drag.current = null }}>
      <span className="peek-icon">{harnessIcon(selected?.harness)}</span>
      <span className="peek-title" tabIndex={0} aria-label="Move terminal with arrow keys"
        onKeyDown={event => {
          const rect = panel.current!.getBoundingClientRect()
          const deltas: Record<string, [number, number]> = { ArrowLeft: [-20, 0], ArrowRight: [20, 0], ArrowUp: [0, -20], ArrowDown: [0, 20] }
          const delta = deltas[event.key]
          if (delta) { event.preventDefault(); move(rect.x + delta[0], rect.y + delta[1]) }
        }}>{selected ? `${selected.nodeTitle} / ${selected.slotLabel}` : 'Terminal Peek'}</span>
      <span className="peek-observer">View only</span>
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
      </button>)}
    </nav>
    {loading ? <p className="peek-message" role="status">Loading run seats…</p>
      : error ? <p className="peek-message" role="alert">{error}</p>
      : terminalAvailable && selected ? <TerminalSurface key={`${selected.createdSeq}-${generation}`} seat={selected} onStateChange={setConnectionState} />
      : <p className="peek-message" role="status">{selected
        ? `${selected.state === 'live' ? 'Observation unavailable' : `Seat ${selected.state}`}. ${selected.reason || ''}`
        : projection?.reason || 'No terminal seats have been created for this formation.'}</p>}
    <footer className="peek-foot"><span role="status">{terminalAvailable ? connectionText[connectionState] : 'View only'}</span>
      <span title="Drag to select text; hold Shift if the terminal uses mouse tracking.">Select and scroll output</span></footer>
  </section>
}
