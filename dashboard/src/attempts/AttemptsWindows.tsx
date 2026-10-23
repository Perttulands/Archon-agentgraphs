import { closes, type FilePlace, type WindowClosing } from '../files/FileWindows'
import { Suspense, createContext, lazy, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowStack } from '../windows/WindowManager'
import { attemptsWindowId, type AttemptsPick } from './attemptsModel'
import { attemptsAnchor } from './attemptsPlacement'
import './attemptsButton.css'

const AttemptsWindow = lazy(() => import('./AttemptsWindow'))

// The cockpit's attempts windows (archon-7963). Every step that ran
// shows <AttemptsButton nodeId={…} /> where it lives, on its card, its Flow row
// and its window; a click opens its attempts. The layer draws the windows
// inside the view's window stack, and a pending best-of-N pick opens its
// step's window to be answered there.

/** What the attempts windows read from the shown run. */
export interface AttemptsRun {
  runId: string
  /** Each step's latest attempt number. */
  counts: ReadonlyMap<string, number>
  /** Changes whenever the run records something. */
  version: string
  /** Whether the run can still add attempts. */
  live: boolean
  /** The gates waiting for the operator now. */
  waitingGates: readonly string[]
  gatesOf?: (nodeId: string) => readonly string[]
  titleOf: (nodeId: string) => string
}

/** A pending pick, for the step whose attempts are its candidates. */
export interface StepPick extends AttemptsPick {
  nodeId: string
  onClose: () => void
}

interface AttemptsState {
  run: AttemptsRun | null
  opened: readonly string[]
  places: ReadonlyMap<string, FilePlace>
  open: (nodeId: string, origin?: Element | null) => void
  close: (nodeId: string) => void
}

const AttemptsContext = createContext<AttemptsState | null>(null)

export const useAttemptsWindows = () => useContext(AttemptsContext)

export function AttemptsWindowsProvider({ run, stack, closing = null, children }: { run: AttemptsRun | null; stack: WindowStack; closing?: WindowClosing | null; children: ReactNode }) {
  const runId = run?.runId || ''
  const places = useRef(new Map<string, FilePlace>())
  // The windows belong to the run they were opened on.
  const [opened, setOpened] = useState<{ runId: string; nodes: readonly string[] }>({ runId: '', nodes: [] })
  const stackRef = useRef(stack)
  stackRef.current = stack
  const open = useCallback((nodeId: string, origin?: Element | null) => {
    if (origin) places.current.set(nodeId, attemptsAnchor(origin, run?.gatesOf?.(nodeId)))
    setOpened(current => current.runId !== runId ? { runId, nodes: [nodeId] }
      : current.nodes.includes(nodeId) ? current : { runId, nodes: [...current.nodes, nodeId] })
    stackRef.current.focus(attemptsWindowId(nodeId))
  }, [runId, run])
  const close = useCallback((nodeId: string) => setOpened(current => ({ ...current, nodes: current.nodes.filter(node => node !== nodeId) })), [])
  // A step's attempts belong to it, and to the step whose window opened them.
  useEffect(() => {
    if (closing) setOpened(current => ({ ...current, nodes: current.nodes.filter(node => !closes(closing, node) && !closes(closing, places.current.get(node)?.owner)) }))
  }, [closing])
  const nodes = opened.runId === runId ? opened.nodes : []
  const value = useMemo(() => ({ run, opened: nodes, places: places.current, open, close }), [close, nodes, open, run])
  return <AttemptsContext.Provider value={value}>{children}</AttemptsContext.Provider>
}

/** "3 attempts" where a step lives; available after the first attempt. */
export function AttemptsButton({ nodeId, className = 'finspect' }: { nodeId: string; className?: string }) {
  const state = useContext(AttemptsContext)
  const count = state?.run?.counts.get(nodeId) || 0
  if (!state?.run || count < 1) return null
  const title = state.run.titleOf(nodeId)
  return (
    <button
      type="button"
      className={`attempts-open ${className}`}
      aria-label={`${count} attempt${count === 1 ? '' : 's'} of ${title}`}
      data-testid={`attempts-open-${nodeId}`}
      onPointerDown={event => event.stopPropagation()}
      onClick={event => {
        event.stopPropagation()
        state.open(nodeId, event.currentTarget)
      }}
    >{count} attempt{count === 1 ? '' : 's'}</button>
  )
}

export function AttemptsWindowsLayer({ pick }: { pick: StepPick | null }) {
  const state = useContext(AttemptsContext)
  const run = state?.run
  const waiting = useMemo(() => new Set(run?.waitingGates || []), [run?.waitingGates])
  if (!state || !run) return null
  const nodes = pick && !state.opened.includes(pick.nodeId) ? [...state.opened, pick.nodeId] : state.opened
  return (
    <>
      {nodes.map(nodeId => {
        const picking = pick?.nodeId === nodeId ? pick : null
        return (
          <Suspense key={`${run.runId}:${nodeId}`} fallback={null}>
            <AttemptsWindow
              runId={run.runId}
              nodeId={nodeId}
              place={state.places.get(nodeId)}
              stepTitle={run.titleOf(nodeId)}
              count={run.counts.get(nodeId) || 0}
              version={run.version}
              live={run.live}
              waitingGates={waiting}
              pick={picking}
              titleOf={run.titleOf}
              onClose={() => {
                picking?.onClose()
                state.close(nodeId)
              }}
            />
          </Suspense>
        )
      })}
    </>
  )
}
