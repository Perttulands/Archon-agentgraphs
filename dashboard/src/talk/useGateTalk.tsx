import { Suspense, lazy, useCallback, useEffect, useMemo, useState } from 'react'
import type { AgentProjection, BoardDocument, RunStatusProjection } from '../components/formationsTypes'
import type { WindowRect } from '../windows/windowGeometry'
import { gateTalk, talkSeatStatus, type GateTalk, type TalkSeat } from './talkModel'

// The terminal is its own chunk, loaded when the operator first talks.
const SeatTalkWindow = lazy(() => import('./SeatTalkWindow'))

interface OpenTalk { seat: TalkSeat; anchor: WindowRect; after?: string; takesFocus: boolean }

export interface GateTalkPanel extends GateTalk {
  /** Opens every asked seat's terminal beside the control, the answer panel. */
  onTalk: (control: Element) => void
}

/**
 * The waiting human gate's talk: what its answer panel offers, and the seat
 * terminals the operator opened from it. Opened windows stay while the run
 * does, so a conversation outlives the answer that ends it.
 */
export function useGateTalk({ board, run, agents, gate, focusWindow }: {
  board: BoardDocument | null | undefined
  run: RunStatusProjection | null | undefined
  agents: readonly AgentProjection[]
  gate: { gateId: string; requestedSeq: number } | null | undefined
  /** Raises a window already open, when Talk is pressed again. */
  focusWindow: (id: string) => void
}) {
  const [open, setOpen] = useState<OpenTalk[]>([])
  const runId = run?.runId || ''
  useEffect(() => { setOpen([]) }, [runId])
  const talk = useMemo(() => gateTalk(board, run, agents, gate), [agents, board, gate, run])

  const onTalk = useCallback((control: Element) => {
    if (!talk) return
    const { left, top, width, height } = control.getBoundingClientRect()
    for (const seat of talk.seats) focusWindow(seat.windowId)
    setOpen(current => {
      const fresh = talk.seats.filter(seat => !current.some(item => item.seat.windowId === seat.windowId))
      // Peers open side by side: each beside the one before it, the first beside the control and typed into first.
      return [...current, ...fresh.map((seat, index) => ({
        seat, anchor: { left, top, width, height }, after: index ? fresh[index - 1].windowId : undefined, takesFocus: index === 0,
      }))]
    })
  }, [focusWindow, talk])

  const panel: GateTalkPanel | null = talk ? { ...talk, onTalk } : null
  const windows = open.length ? (
    <Suspense fallback={null}>
      {open.map(item => (
        <SeatTalkWindow key={item.seat.windowId} seat={item.seat} status={talkSeatStatus(run, item.seat)} focusOnOpen={item.takesFocus}
          anchor={() => windowRect(item.after) || item.anchor}
          onClose={() => setOpen(current => current.filter(other => other.seat.windowId !== item.seat.windowId))} />
      ))}
    </Suspense>
  ) : null
  return { panel, windows }
}

function windowRect(windowId: string | undefined): WindowRect | null {
  if (!windowId) return null
  const element = document.querySelector(`[data-window-id="${windowId}"]`)
  if (!element) return null
  const { left, top, width, height } = element.getBoundingClientRect()
  return { left, top, width, height }
}
