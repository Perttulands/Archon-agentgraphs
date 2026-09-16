/**
 * The size rule for floating windows, and where a resized one is remembered.
 *
 * Ported from CHROTE (chrote/dashboard/src/hooks/floatingWindowSize.ts). A
 * window opens at its default size until the operator resizes one of its
 * kind. From then on the remembered size for that kind decides, on this
 * device, for every later window of the kind.
 *
 * The minimum belongs to the gesture: a dragged size is held at the minimum so
 * the window cannot be dragged away to nothing.
 */

/** Peek holds a seat terminal; the other kinds are reserved for node, note and file windows. */
export type FloatingWindowKind = 'peek' | 'node' | 'note' | 'file'

export interface FrameSize {
  width: number
  height: number
}

const STORAGE_KEY = 'archon.floatingWindowSize.v1'
const STORAGE_VERSION = 1

/** The smallest a dragged window of each kind may be: a header and a look. */
export const FLOATING_WINDOW_MINIMUM: Record<FloatingWindowKind, FrameSize> = {
  peek: { width: 240, height: 120 },
  node: { width: 320, height: 160 },
  note: { width: 280, height: 140 },
  // A file is read in lines, so its window is kept wide enough for one.
  file: { width: 320, height: 120 },
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function readSizes(): Record<string, unknown> {
  if (typeof window === 'undefined') return {}
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(STORAGE_KEY) || 'null')
    if (!isRecord(parsed) || parsed.version !== STORAGE_VERSION || !isRecord(parsed.sizes)) return {}
    return parsed.sizes
  } catch {
    return {}
  }
}

function writeSizes(sizes: Record<string, unknown>): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: STORAGE_VERSION, sizes }))
  } catch {
    // Private mode and quota failures must not make a window unopenable.
  }
}

function sanitizeSize(value: unknown): FrameSize | null {
  if (!isRecord(value)) return null
  const { width, height } = value
  if (typeof width !== 'number' || typeof height !== 'number') return null
  if (!Number.isFinite(width) || !Number.isFinite(height)) return null
  if (width <= 0 || height <= 0) return null
  return { width, height }
}

/** The size the operator last dragged this kind of window to, if any. */
export function readFloatingWindowSize(kind: FloatingWindowKind): FrameSize | null {
  return sanitizeSize(readSizes()[kind])
}

export function writeFloatingWindowSize(kind: FloatingWindowKind, size: FrameSize): void {
  const sanitized = sanitizeSize(size)
  if (!sanitized) return
  writeSizes({ ...readSizes(), [kind]: { width: Math.round(sanitized.width), height: Math.round(sanitized.height) } })
}

export function clearFloatingWindowSize(kind: FloatingWindowKind): void {
  const sizes = readSizes()
  if (!(kind in sizes)) return
  delete sizes[kind]
  writeSizes(sizes)
}

function clampSide(length: number, minimum: number, maximum: number): number {
  const floor = Math.max(1, Number.isFinite(minimum) ? minimum : 1)
  const ceiling = Math.max(floor, Number.isFinite(maximum) ? maximum : Number.POSITIVE_INFINITY)
  return Math.min(ceiling, Math.max(floor, Number.isFinite(length) ? length : floor))
}

/**
 * Hold a size inside the workspace and above the minimum. The minimum wins
 * over a workspace smaller than it, because a window clipped by its workspace
 * is still readable and a window of nothing is not.
 */
export function clampFrameSize(size: FrameSize, minimum: FrameSize, bounds: FrameSize | null): FrameSize {
  return {
    width: clampSide(size.width, minimum.width, bounds ? bounds.width : Number.POSITIVE_INFINITY),
    height: clampSide(size.height, minimum.height, bounds ? bounds.height : Number.POSITIVE_INFINITY),
  }
}
