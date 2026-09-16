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
import { WINDOW_Z_BASE, useWindowStack } from './WindowManager'
import { keepInWorkspace, moveWindowRect, placeWindow, resizeWindowRect, type HandleAxis, type WindowRect } from './windowGeometry'

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
  /** What the window is called in its handles' labels: "terminal Peek". */
  label: string
  defaultSize: FrameSize
  minimum?: FrameSize
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
}

// A press on a control in the title bar uses the control, not the window.
const CONTROL = 'button,select,input,textarea,a,[role="separator"]'

export function useFloatingWindow<T extends HTMLElement = HTMLElement>({
  id,
  kind,
  label,
  defaultSize,
  minimum = FLOATING_WINDOW_MINIMUM[kind],
  onClose,
}: UseFloatingWindowOptions): FloatingWindow<T> {
  const stack = useWindowStack()
  const { openCount, register, workspace } = stack
  const ref = useRef<T>(null)
  // Placed as it first renders, so it is visible, and can take focus, from its first paint.
  const placedAt = useRef({ cascade: -1, size: defaultSize })
  const [rect, setRect] = useState<WindowRect>(() => {
    placedAt.current = { cascade: openCount(), size: readFloatingWindowSize(kind) ?? defaultSize }
    return placeWindow(placedAt.current.size, workspace(), minimum, placedAt.current.cascade)
  })
  const rectRef = useRef(rect)
  rectRef.current = rect
  const [activeHandle, setActiveHandle] = useState<FrameHandleId | null>(null)
  const cleanupRef = useRef<(() => void) | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const placement = useRef({ kind, minimum })
  placement.current = { kind, minimum }

  // Windows opened in the same render all counted the same windows before them,
  // so each steps past the ones registered first before it paints.
  useLayoutEffect(() => {
    const cascade = openCount()
    if (cascade !== placedAt.current.cascade) {
      placedAt.current = { ...placedAt.current, cascade }
      setRect(placeWindow(placedAt.current.size, workspace(), placement.current.minimum, cascade))
    }
    return register(id)
  }, [id, openCount, register, workspace])

  useEffect(() => {
    const hold = () => setRect(current => keepInWorkspace(current, workspace(), placement.current.minimum))
    window.addEventListener('resize', hold)
    return () => window.removeEventListener('resize', hold)
  }, [workspace])

  useEffect(() => () => {
    cleanupRef.current?.()
    cleanupRef.current = null
  }, [])

  const remember = useCallback((size: WindowRect) => {
    writeFloatingWindowSize(placement.current.kind, { width: size.width, height: size.height })
  }, [])

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
      let last = start
      setActiveHandle(direction.id)
      const end = (keep: boolean) => {
        cleanupRef.current?.()
        cleanupRef.current = null
        setActiveHandle(null)
        if (keep && last !== start) remember(last)
        if (!keep) setRect(start)
      }
      cleanupRef.current = capturePointerDrag(event.currentTarget, event.pointerId, {
        move: moveEvent => {
          const next = resizeWindowRect(start, direction, moveEvent.clientX - grabbedAt.x, moveEvent.clientY - grabbedAt.y, area, placement.current.minimum)
          if (!next) return
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
        // Keys typed in a window belong to it, not to the cockpit's shortcuts.
        event.stopPropagation()
        if (event.key !== 'Escape' || event.defaultPrevented) return
        event.preventDefault()
        onCloseRef.current()
      },
    },
    moveProps: { onPointerDown: beginMove },
    onMoveKeyDown,
  }
}
