/* The agent roster collapses to a rail and resizes from its right edge. Its
 * width and collapsed state are remembered in this browser. */
import { useCallback, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react'
import '../styles/formations-roster.css'

export const ROSTER_WIDTH_KEY = 'chrote-formations-roster-width'
export const ROSTER_COLLAPSED_KEY = 'chrote-formations-roster-collapsed'
export const ROSTER_DEFAULT_WIDTH = 236
export const ROSTER_MIN_WIDTH = 180
export const ROSTER_MAX_WIDTH = 480
const ROSTER_KEY_STEP = 16

export function clampRosterWidth(width: number): number {
  if (!Number.isFinite(width)) return ROSTER_DEFAULT_WIDTH
  return Math.round(Math.min(ROSTER_MAX_WIDTH, Math.max(ROSTER_MIN_WIDTH, width)))
}

function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage may be unavailable; the panel still works for this page.
  }
}

export function useRosterPanel() {
  const [width, setWidth] = useState(() => {
    const stored = readStored(ROSTER_WIDTH_KEY)
    return stored === null ? ROSTER_DEFAULT_WIDTH : clampRosterWidth(Number(stored))
  })
  const [collapsed, setCollapsed] = useState(() => readStored(ROSTER_COLLAPSED_KEY) === 'true')

  const resizeTo = useCallback((next: number) => {
    const clamped = clampRosterWidth(next)
    setWidth(clamped)
    writeStored(ROSTER_WIDTH_KEY, String(clamped))
  }, [])

  const toggle = useCallback(() => {
    setCollapsed(current => {
      writeStored(ROSTER_COLLAPSED_KEY, String(!current))
      return !current
    })
  }, [])

  const beginResize = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    event.stopPropagation()
    const startX = event.clientX
    const startWidth = width
    const onMove = (move: PointerEvent) => setWidth(clampRosterWidth(startWidth + move.clientX - startX))
    const onUp = (up: PointerEvent) => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      resizeTo(startWidth + up.clientX - startX)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }, [resizeTo, width])

  const resizeByKey = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    const next = {
      ArrowLeft: width - ROSTER_KEY_STEP,
      ArrowRight: width + ROSTER_KEY_STEP,
      Home: ROSTER_MIN_WIDTH,
      End: ROSTER_MAX_WIDTH,
    }[event.key]
    if (next === undefined) return
    event.preventDefault()
    resizeTo(next)
  }, [resizeTo, width])

  return { width, collapsed, toggle, beginResize, resizeByKey }
}
