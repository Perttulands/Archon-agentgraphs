import { useEffect, useRef, useState, type ReactNode } from 'react'
import { GateDraftSavedContext } from './HumanGateAnswerPanel'
import FloatingFrameHandles from '../windows/FloatingFrameHandles'
import { useFloatingWindow } from '../windows/useFloatingWindow'
import { useWindowStack } from '../windows/WindowManager'
import type { WindowRect } from '../windows/windowGeometry'
import '../windows/floatingWindows.css'

// The pending human gate's answer on the canvas, as a floating window
// (archon-n7u.7): the operator moves, resizes or closes it like any other, and it
// opens clear of the gate and the steps Approve leads to, so the consequence of
// the decision stays in view. The gate's own node window never covers it: when
// that window opens, this one is raised above it once.

export const GATE_ANSWER_WINDOW_ID = 'gate-answer'

/** The cards of the given nodes on the canvas, in viewport pixels; nodes not drawn are left out. */
export function cardRects(nodeIds: readonly string[], root: ParentNode = document): WindowRect[] {
  return nodeIds
    .map(nodeId => root.querySelector(`[data-node="${nodeId.replace(/["\\]/g, '\\$&')}"]`))
    .filter((card): card is Element => Boolean(card))
    .map(card => card.getBoundingClientRect())
    .filter(box => box.width > 0 && box.height > 0)
    .map(({ left, top, width, height }) => ({ left, top, width, height }))
}

export default function GateAnswerWindow({ gateId, gateTitle, anchor, keepClear, focusRequest = 0, onClose, children }: {
  gateId: string
  gateTitle: string
  /** The gate's card, which the window opens beside. */
  anchor: () => WindowRect | null
  /** The steps the answer leads to, which stay in view. */
  keepClear: () => readonly WindowRect[]
  /** Counts the operator's requests to bring the answer up; each takes the keyboard. A run reaching the gate does not. */
  focusRequest?: number
  onClose: () => void
  children: ReactNode
}) {
  const label = `Answer gate ${gateTitle}`
  const win = useFloatingWindow<HTMLElement>({
    id: GATE_ANSWER_WINDOW_ID,
    kind: 'answer',
    label,
    defaultSize: { width: 460, height: 640 },
    anchor,
    keepClear,
    onClose,
  })
  const { ref } = win.rootProps
  const [draftSaved, setDraftSaved] = useState(true)
  const stack = useWindowStack()
  const { order, focus } = stack

  useEffect(() => {
    if (focusRequest) ref.current?.querySelector<HTMLTextAreaElement>('textarea:not(:disabled)')?.focus({ preventScroll: true })
  }, [ref, focusRequest])

  // Raise this window once whenever the gate's node window opens above it.
  const gateWindowId = `node:${gateId}`
  const gateWindowOpen = order.includes(gateWindowId)
  const wasOpen = useRef(gateWindowOpen)
  useEffect(() => {
    const opened = gateWindowOpen && !wasOpen.current
    wasOpen.current = gateWindowOpen
    if (opened && order.indexOf(gateWindowId) > order.indexOf(GATE_ANSWER_WINDOW_ID)) focus(GATE_ANSWER_WINDOW_ID)
  }, [focus, gateWindowId, gateWindowOpen, order])

  return (
    <section
      {...win.rootProps}
      className={`fwin gate-answer-window${win.focused ? ' focused' : ''}`}
      role="dialog"
      aria-label={label}
      data-window-id={GATE_ANSWER_WINDOW_ID}
      data-window-kind="answer"
      data-testid="gate-answer-window"
    >
      <header className="fwin-head" {...win.moveProps}>
        <span className="fwin-title" tabIndex={0} aria-label={`Move ${label} with arrow keys`} onKeyDown={win.onMoveKeyDown}>
          <span className="gate-answer-kicker">Needs your answer</span> {gateTitle}
        </span>
        <button type="button" className="fwin-close" aria-label={`Close ${label}`} title={draftSaved ? 'Close. Your draft stays in this browser; reopen the answer from the run bar.' : 'Close. Your draft could not be saved in this browser and is lost when you close.'}
          onClick={onClose}>×</button>
      </header>
      <div className="fwin-body"><GateDraftSavedContext.Provider value={setDraftSaved}>{children}</GateDraftSavedContext.Provider></div>
      <FloatingFrameHandles handles={win.handles} activeHandle={win.activeHandle} />
    </section>
  )
}
