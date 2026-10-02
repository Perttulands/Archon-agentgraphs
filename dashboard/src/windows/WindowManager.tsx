import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import type { WindowRect, Workspace } from './windowGeometry'

/**
 * The floating windows open over one view: which is on top, and the workspace
 * they are held inside. A view creates the stack with useWindowManager and
 * provides it; each window registers itself while it is open and is rendered
 * wherever the view's props are fresh. Focusing a window, by pointer or
 * keyboard, raises it above the others.
 */

/** The z-index of the lowest window; each window above it adds one. */
export const WINDOW_Z_BASE = 180

export interface WindowStack {
  /** Open window ids, bottom to top. */
  order: readonly string[]
  register: (id: string) => () => void
  /** Raise an open window to the top. */
  focus: (id: string) => void
  isOpen: (id: string) => boolean
  openCount: () => number
  workspace: (overCanvas?: boolean) => Workspace
  /** Where each open window sits now. A window reports its place with `track` and forgets it when it closes. */
  track: (id: string, rect: WindowRect) => void
  /** The open windows other than `except`, bottom to top. */
  openRects: (except?: string) => WindowRect[]
  /** What the view shows under its windows, measured now; a new window prefers not to cover it. See windowPlacement.ts. */
  scene: () => ViewScene
  /**
   * Hold every open window inside the workspace again, after it changed
   * without the page resizing: the run bar appeared, or a panel collapsed.
   */
  reflow: () => void
  /** Called on each reflow; returns the unsubscribe. */
  onReflow: (listener: () => void) => () => void
}

/** What a view shows under its windows, for placing a new one: see PlacementScene. */
export interface ViewScene {
  landmarks?: readonly WindowRect[]
  content?: readonly WindowRect[]
}

const WindowStackContext = createContext<WindowStack | null>(null)

const INSET = 8

/** The viewport, for a view that has not said where its windows belong. */
export function viewportWorkspace(): Workspace {
  return {
    bounds: { left: INSET, top: INSET, width: Math.max(0, window.innerWidth - 2 * INSET), height: Math.max(0, window.innerHeight - 2 * INSET) },
    avoid: [],
  }
}

/**
 * A view's window stack. `workspace` is measured when a window opens, moves or
 * resizes; null means the viewport. `scene` is measured when a window opens.
 */
export function useWindowManager(workspace?: (overCanvas?: boolean) => Workspace | null, scene?: () => ViewScene): WindowStack {
  const ids = useRef<string[]>([])
  const rects = useRef(new Map<string, WindowRect>())
  const [order, setOrder] = useState<readonly string[]>([])
  const workspaceRef = useRef(workspace)
  workspaceRef.current = workspace
  const sceneRef = useRef(scene)
  sceneRef.current = scene

  const raise = useCallback((id: string) => {
    ids.current = [...ids.current.filter(open => open !== id), id]
    setOrder(ids.current)
  }, [])

  const register = useCallback((id: string) => {
    raise(id)
    return () => {
      ids.current = ids.current.filter(open => open !== id)
      rects.current.delete(id)
      setOrder(ids.current)
    }
  }, [raise])

  const focus = useCallback((id: string) => {
    if (!ids.current.includes(id) || ids.current[ids.current.length - 1] === id) return
    raise(id)
  }, [raise])

  const isOpen = useCallback((id: string) => ids.current.includes(id), [])
  const openCount = useCallback(() => ids.current.length, [])
  const measure = useCallback((overCanvas = false) => workspaceRef.current?.(overCanvas) ?? viewportWorkspace(), [])
  const track = useCallback((id: string, rect: WindowRect) => {
    if (ids.current.includes(id)) rects.current.set(id, rect)
  }, [])
  const openRects = useCallback((except?: string) => ids.current
    .filter(id => id !== except)
    .map(id => rects.current.get(id))
    .filter((rect): rect is WindowRect => Boolean(rect)), [])
  const measureScene = useCallback(() => sceneRef.current?.() ?? {}, [])
  const listeners = useRef(new Set<() => void>())
  const reflow = useCallback(() => listeners.current.forEach(listener => listener()), [])
  const onReflow = useCallback((listener: () => void) => {
    listeners.current.add(listener)
    return () => { listeners.current.delete(listener) }
  }, [])

  // Only the order changes while windows are open; the functions stay the same,
  // so a window is placed once and not again whenever another is raised.
  return useMemo<WindowStack>(
    () => ({ order, register, focus, isOpen, openCount, workspace: measure, track, openRects, scene: measureScene, reflow, onReflow }),
    [focus, isOpen, measure, measureScene, onReflow, openCount, openRects, order, reflow, register, track],
  )
}

export function WindowManagerProvider({ stack, children }: { stack: WindowStack; children: ReactNode }) {
  return <WindowStackContext.Provider value={stack}>{children}</WindowStackContext.Provider>
}

export function useWindowStack(): WindowStack {
  const stack = useContext(WindowStackContext)
  if (!stack) throw new Error('A floating window must be rendered inside WindowManagerProvider')
  return stack
}
