/**
 * One floating window: where it sits, and the gestures that move and resize it.
 *
 * Adapted from CHROTE's floating frame (chrote/dashboard/src/hooks/
 * useFloatingFrame.ts): eight handles, edges before corners, a 16px arrow-key
 * step, the minimum per kind and the remembered size. CHROTE centres a single
 * window; here several windows are placed, moved by their title bars and
 * stacked, so a handle moves only the edges it holds and the window keeps its
 * other edges where they were.
 *
 * The caller renders the window. It spreads `rootProps` on the window element,
 * `moveProps` on the title bar, and draws `handles` with FloatingFrameHandles.
 */

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, FocusEvent, KeyboardEvent, KeyboardEventHandler, PointerEvent, PointerEventHandler, RefObject } from 'react'
import { RESIZE_KEYBOARD_STEP, capturePointerDrag } from './resizeGesture'
import {
  FLOATING_WINDOW_MINIMUM,
  readFloatingWindowSize,
  writeFloatingWindowSize,
  type FloatingWindowKind,
  type FrameSize,
} from './floatingWindowSize'
import { WINDOW_Z_BASE, useWindowStack, type WindowStack } from './WindowManager'
import { keepInWorkspace, moveWindowRect, resizeWindowRect, type HandleAxis, type WindowRect } from './windowGeometry'
import { placeOpeningWindow } from './windowPlacement'

export type FrameHandleId = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'

interface HandleDirection extends HandleAxis {
  id: FrameHandleId
  where: string
}

// Edges first, corners after, so a corner wins the overlap at the corner.
const HANDLES: readonly HandleDirection[] = [
  { id: 'n', x: 0, y: -1, where: 'top' },
  { id: 's', x: 0, y: 1, where: 'bottom' },
  { id: 'w', x: -1, y: 0, where: 'left' },
  { id: 'e', x: 1, y: 0, where: 'right' },
  { id: 'nw', x: -1, y: -1, where: 'top left' },
  { id: 'ne', x: 1, y: -1, where: 'top right' },
  { id: 'sw', x: -1, y: 1, where: 'bottom left' },
  { id: 'se', x: 1, y: 1, where: 'bottom right' },
]

/** How far an arrow key moves a window whose title has focus. */
export const MOVE_KEYBOARD_STEP = 20

export interface FloatingFrameHandleProps {
  onPointerDown: PointerEventHandler<HTMLDivElement>
  onKeyDown: KeyboardEventHandler<HTMLDivElement>
  role: 'separator'
  'aria-label': string
  'aria-orientation'?: 'horizontal' | 'vertical'
  tabIndex: 0
}

export interface FloatingFrameHandle {
  id: FrameHandleId
  props: FloatingFrameHandleProps
}

export interface UseFloatingWindowOptions {
  /** Stable while the window is open; opening the same id again reuses its place in the stack. */
  id: string
  kind: FloatingWindowKind
  /** What the window is called in its handles' labels: "terminal". */
  label: string
  defaultSize: FrameSize
  minimum?: FrameSize
  /**
   * Where the thing the window belongs to sits, in viewport pixels, read as the
   * window opens. The window opens in free space near it and never covers it
   * when it can avoid it; without one the window opens near the centre.
   */
  anchor?: () => WindowRect | null
  /**
   * What the window must leave visible and clickable, read as it opens: the
   * node's neighbours, its next link, or a downstream node the window talks
   * about. See windowPlacement.ts.
   */
  keepClear?: () => readonly WindowRect[]
  /** Whether the anchor is a node on the canvas or a control outside it, such as a run bar chip; see PlacementScene. */
  anchorKind?: 'node' | 'control'
  /** CHROTE Peek: open over the canvas, clear of existing title bars. */
  overCanvas?: boolean
  onClose: () => void
}

export interface FloatingWindow<T extends HTMLElement> {
  rect: WindowRect
  focused: boolean
  resizing: boolean
  /** The handle in hand, so only that one shows it is being dragged. */
  activeHandle: FrameHandleId | null
  handles: readonly FloatingFrameHandle[]
  rootProps: {
    ref: RefObject<T>
    tabIndex: -1
    style: CSSProperties
    onPointerDownCapture: PointerEventHandler<T>
    onPointerDown: PointerEventHandler<T>
    onFocusCapture: (event: FocusEvent<T>) => void
    onKeyDown: KeyboardEventHandler<T>
  }
  moveProps: { onPointerDown: PointerEventHandler<HTMLElement> }
  /** Arrow keys on a focusable title move the window. */
  onMoveKeyDown: KeyboardEventHandler<HTMLElement>
  /**
   * Wrap the window around its content, keeping its top left, as CHROTE's Peek
   * shrinks to the grid its font fit drew (chrote-8eyu). Ignored once the
   * operator has sized a window of this kind, whose size then wins.
   */
  fitTo: (size: FrameSize) => void
  /** Whether the operator has sized a window of this kind, now or before it opened. */
  sizedByOperator: boolean
}

// A press on a control in the title bar uses the control, not the window.
const CONTROL = 'button,select,input,textarea,a,[role="separator"]'

function openingRect(stack: WindowStack, id: string, size: FrameSize, minimum: FrameSize, anchor?: () => WindowRect | null, keepClear?: () => readonly WindowRect[], anchorKind?: 'node' | 'control', overCanvas = false): WindowRect {
  if (overCanvas) {
    const workspace = stack.workspace()
    const bounds = workspace.bounds
    return placeOpeningWindow({ width: Math.min(size.width, Math.floor(bounds.width * .9)), height: Math.min(size.height, Math.floor(bounds.height * .9)) }, minimum, {
      workspace, windows: stack.openRects(id).map(rect => ({ ...rect, height: Math.min(40, rect.height) })),
    })
  }
  return placeOpeningWindow(size, minimum, {
    workspace: stack.workspace(),
    anchor: anchor?.() ?? null,
    anchorKind,
    keepClear: keepClear?.() ?? [],
    windows: stack.openRects(id),
    ...stack.scene(),
  })
}

export function useFloatingWindow<T extends HTMLElement = HTMLElement>({
  id,
  kind,
  label,
  defaultSize,
  minimum = FLOATING_WINDOW_MINIMUM[kind],
  anchor,
  keepClear,
  anchorKind,
  overCanvas = false,
  onClose,
}: UseFloatingWindowOptions): FloatingWindow<T> {
  const stack = useWindowStack()
  const { register, track, workspace } = stack
  const stackRef = useRef(stack)
  stackRef.current = stack
  const ref = useRef<T>(null)
  // Placed as it first renders, so it is visible, and can take focus, from its first paint.
  const placedAt = useRef({ others: -1, size: defaultSize })
  const [rect, setRect] = useState<WindowRect>(() => {
    placedAt.current = { others: stack.openRects(id).length, size: readFloatingWindowSize(kind) ?? defaultSize }
    return openingRect(stack, id, placedAt.current.size, minimum, anchor, keepClear, anchorKind, overCanvas)
  })
  const rectRef = useRef(rect)
  rectRef.current = rect
  const [activeHandle, setActiveHandle] = useState<FrameHandleId | null>(null)
  // A remembered size is the operator's, so it wins from the first paint.
  const [sizedByOperator, setSizedByOperator] = useState(() => readFloatingWindowSize(kind) !== null)
  const sized = useRef(sizedByOperator)
  const cleanupRef = useRef<(() => void) | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const placement = useRef({ kind, minimum, anchor, keepClear, anchorKind, overCanvas })
  placement.current = { kind, minimum, anchor, keepClear, anchorKind, overCanvas }

  // The stack knows where every open window is, so the next one opens clear
  // of it. Before the window registers this does nothing; registering tracks it.
  useLayoutEffect(() => {
    track(id, rect)
  }, [id, rect, track])

  // Windows opened in the same render were all placed before any of them was
  // open, so each makes room for the ones registered first before it paints.
  useLayoutEffect(() => {
    const unregister = register(id)
    const others = stackRef.current.openRects(id).length
    if (others !== placedAt.current.others) {
      placedAt.current = { ...placedAt.current, others }
      const { minimum: least, anchor: beside, keepClear: clear, anchorKind: besideKind } = placement.current
      const placed = openingRect(stackRef.current, id, placedAt.current.size, least, beside, clear, besideKind, placement.current.overCanvas)
      rectRef.current = placed
      setRect(placed)
    }
    // Tracked now, so a window opened in the same render after this one makes room for where this one is going.
    track(id, rectRef.current)
    return unregister
  }, [id, register, track])

  const { onReflow } = stack
  useEffect(() => {
    // A window already inside keeps its rect, so a reflow on every frame of a
    // drag or resize elsewhere re-renders nothing, a Peek terminal included.
    const hold = () => setRect(current => {
      const held = keepInWorkspace(current, workspace(), placement.current.minimum)
      return held.left === current.left && held.top === current.top && held.width === current.width && held.height === current.height ? current : held
    })
    window.addEventListener('resize', hold)
    const stop = onReflow(hold)
    return () => {
      window.removeEventListener('resize', hold)
      stop()
    }
  }, [onReflow, workspace])

  useEffect(() => () => {
    cleanupRef.current?.()
    cleanupRef.current = null
  }, [])

  const remember = useCallback((size: WindowRect) => {
    sized.current = true
    setSizedByOperator(true)
    writeFloatingWindowSize(placement.current.kind, { width: size.width, height: size.height })
  }, [])

  const fitTo = useCallback((size: FrameSize) => {
    if (sized.current) return
    setRect(current => {
      if (current.width === size.width && current.height === size.height) return current
      return keepInWorkspace({ ...current, width: size.width, height: size.height }, workspace(), placement.current.minimum)
    })
  }, [workspace])

  const handleProps = useCallback((direction: HandleDirection): FloatingFrameHandleProps => ({
    role: 'separator',
    tabIndex: 0,
    'aria-label': `Resize the ${label} from the ${direction.where}`,
    ...(direction.x === 0 ? { 'aria-orientation': 'horizontal' as const } : {}),
    ...(direction.y === 0 ? { 'aria-orientation': 'vertical' as const } : {}),
    onPointerDown: event => {
      const start = rectRef.current
      if (event.button !== 0) return
      event.preventDefault()
      event.stopPropagation()
      cleanupRef.current?.()
      const grabbedAt = { x: event.clientX, y: event.clientY }
      const area = workspace()
      const wasSized = sized.current
      let last = start
      setActiveHandle(direction.id)
      const end = (keep: boolean) => {
        cleanupRef.current?.()
        cleanupRef.current = null
        setActiveHandle(null)
        if (keep && last !== start) remember(last)
        if (!keep) {
          setRect(start)
          // A cancelled first drag is not the operator sizing the window.
          if (!wasSized) {
            sized.current = false
            setSizedByOperator(false)
          }
        }
      }
      cleanupRef.current = capturePointerDrag(event.currentTarget, event.pointerId, {
        move: moveEvent => {
          const next = resizeWindowRect(start, direction, moveEvent.clientX - grabbedAt.x, moveEvent.clientY - grabbedAt.y, area, placement.current.minimum)
          if (!next) return
          // The operator is sizing the window from the first move: stop any
          // self-fitting (fitTo) so it cannot pull the window back mid-drag,
          // as CHROTE's Peek follows the dragged size while resizing.
          if (!sized.current) {
            sized.current = true
            setSizedByOperator(true)
          }
          last = next
          setRect(next)
        },
        finish: () => end(true),
        cancel: () => end(false),
      })
    },
    onKeyDown: event => {
      const horizontal = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
      const vertical = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
      const dx = direction.x === 0 ? 0 : horizontal * RESIZE_KEYBOARD_STEP
      const dy = direction.y === 0 ? 0 : vertical * RESIZE_KEYBOARD_STEP
      if (dx === 0 && dy === 0) return
      event.preventDefault()
      const next = resizeWindowRect(rectRef.current, direction, dx, dy, workspace(), placement.current.minimum)
      if (!next) return
      setRect(next)
      remember(next)
    },
  }), [label, remember, workspace])

  const focus = stack.focus
  const beginMove = useCallback((event: PointerEvent<HTMLElement>) => {
    const start = rectRef.current
    if (event.button !== 0 || (event.target as HTMLElement).closest(CONTROL)) return
    event.preventDefault()
    cleanupRef.current?.()
    const grabbedAt = { x: event.clientX, y: event.clientY }
    const area = workspace()
    const end = () => {
      cleanupRef.current?.()
      cleanupRef.current = null
    }
    cleanupRef.current = capturePointerDrag(event.currentTarget, event.pointerId, {
      move: moveEvent => setRect(moveWindowRect(start, moveEvent.clientX - grabbedAt.x, moveEvent.clientY - grabbedAt.y, area, placement.current.minimum)),
      finish: end,
      cancel: () => { end(); setRect(start) },
    })
  }, [workspace])

  const onMoveKeyDown = useCallback((event: KeyboardEvent<HTMLElement>) => {
    const deltas: Record<string, [number, number]> = {
      ArrowLeft: [-MOVE_KEYBOARD_STEP, 0], ArrowRight: [MOVE_KEYBOARD_STEP, 0], ArrowUp: [0, -MOVE_KEYBOARD_STEP], ArrowDown: [0, MOVE_KEYBOARD_STEP],
    }
    const delta = deltas[event.key]
    if (!delta) return
    event.preventDefault()
    setRect(moveWindowRect(rectRef.current, delta[0], delta[1], workspace(), placement.current.minimum))
  }, [workspace])

  const stackIndex = stack.order.indexOf(id)
  return {
    rect,
    focused: stackIndex >= 0 && stackIndex === stack.order.length - 1,
    resizing: activeHandle !== null,
    activeHandle,
    handles: HANDLES.map(direction => ({ id: direction.id, props: handleProps(direction) })),
    rootProps: {
      ref,
      tabIndex: -1,
      style: { position: 'fixed', left: rect.left, top: rect.top, width: rect.width, height: rect.height, zIndex: WINDOW_Z_BASE + Math.max(0, stackIndex) },
      onPointerDownCapture: event => {
        focus(id)
        // Keyboard focus follows the pointer into the window, so Escape closes this one.
        const root = ref.current
        if (root && !root.contains(document.activeElement) && !(event.target as HTMLElement).closest(CONTROL)) root.focus({ preventScroll: true })
      },
      // The canvas under a window never sees its presses.
      onPointerDown: event => event.stopPropagation(),
      onFocusCapture: () => focus(id),
      onKeyDown: event => {
        // Keys typed in a window belong to it, not to the view's shortcuts. Undo
        // still reaches the view, so an edit made in a window can be undone from it.
        if ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === 'z') return
        event.stopPropagation()
        if (event.key !== 'Escape' || event.defaultPrevented) return
        event.preventDefault()
        onCloseRef.current()
      },
    },
    moveProps: { onPointerDown: beginMove },
    onMoveKeyDown,
    fitTo,
    sizedByOperator,
  }
}
