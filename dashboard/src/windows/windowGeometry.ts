import { clampFrameSize, type FrameSize } from './floatingWindowSize'

/**
 * Where a floating window sits. Rectangles are in viewport pixels, because
 * windows are fixed over the page.
 *
 * A workspace is the area windows are held inside, plus zones they must stay
 * off: the cockpit keeps windows on its canvas and away from the zoom column.
 */

export interface WindowRect {
  left: number
  top: number
  width: number
  height: number
}

export interface Workspace {
  bounds: WindowRect
  avoid: readonly WindowRect[]
}

/** A handle's direction: -1 west or north, 1 east or south, 0 neither. */
export interface HandleAxis {
  x: -1 | 0 | 1
  y: -1 | 0 | 1
}

/** How far a newly opened window steps down and right from the last one. */
export const CASCADE_STEP = 28

const right = (rect: WindowRect) => rect.left + rect.width
const bottom = (rect: WindowRect) => rect.top + rect.height
const area = (rect: WindowRect) => rect.width * rect.height

// Bounds win over a window too big to fit: it starts at the near edge.
const clamp = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value))

export function rectsOverlap(a: WindowRect, b: WindowRect): boolean {
  return a.left < right(b) && right(a) > b.left && a.top < bottom(b) && bottom(a) > b.top
}

function inside(rect: WindowRect, bounds: WindowRect): boolean {
  return rect.left >= bounds.left && rect.top >= bounds.top && right(rect) <= right(bounds) && bottom(rect) <= bottom(bounds)
}

function clear(rect: WindowRect, workspace: Workspace): boolean {
  return workspace.avoid.every(zone => !rectsOverlap(rect, zone))
}

/**
 * Hold a window inside the workspace and off its avoided zones. The window
 * moves the shortest way clear of a zone; when no move clears it, it narrows
 * or shortens beside the zone, down to its minimum.
 */
export function keepInWorkspace(rect: WindowRect, workspace: Workspace, minimum: FrameSize): WindowRect {
  const { bounds } = workspace
  const size = clampFrameSize(rect, minimum, bounds)
  let next: WindowRect = {
    ...size,
    left: clamp(rect.left, bounds.left, right(bounds) - size.width),
    top: clamp(rect.top, bounds.top, bottom(bounds) - size.height),
  }
  for (const zone of workspace.avoid) {
    if (!rectsOverlap(next, zone)) continue
    const current = next
    const moves: WindowRect[] = [
      { ...current, left: zone.left - current.width },
      { ...current, left: right(zone) },
      { ...current, top: zone.top - current.height },
      { ...current, top: bottom(zone) },
    ].filter(candidate => inside(candidate, bounds) && clear(candidate, workspace))
    const distance = (candidate: WindowRect) => Math.abs(candidate.left - current.left) + Math.abs(candidate.top - current.top)
    const moved = moves.sort((a, b) => distance(a) - distance(b))[0]
    if (moved) {
      next = moved
      continue
    }
    const shrunk = [
      { ...current, width: zone.left - current.left },
      { ...current, height: zone.top - current.top },
    ].filter(candidate => candidate.width >= minimum.width && candidate.height >= minimum.height && clear(candidate, workspace))
    next = shrunk.sort((a, b) => area(b) - area(a))[0] || current
  }
  return next
}

/** A new window: centred in the workspace, stepped down and right past the windows already open. */
export function placeWindow(size: FrameSize, workspace: Workspace, minimum: FrameSize, cascade: number): WindowRect {
  const { bounds } = workspace
  const fitted = clampFrameSize(size, minimum, bounds)
  const step = CASCADE_STEP * cascade
  return keepInWorkspace({
    ...fitted,
    left: bounds.left + (bounds.width - fitted.width) / 2 + step,
    top: bounds.top + (bounds.height - fitted.height) / 2 + step,
  }, workspace, minimum)
}

/** Room left between a window and the thing it opened beside. */
export const ANCHOR_GAP = 12

/** How far outside the workspace an anchor still counts as in view, such as a chip in a bar along its edge. */
export const ANCHOR_REACH = 48

/**
 * A new window beside what it belongs to: right of it, else left of it, below
 * or above, whichever side first fits the workspace without covering it. The
 * window lines up with the anchor's top or left edge. An anchor just outside
 * the workspace tries the side facing the workspace first. Null when the anchor
 * is empty, out of view, or no side leaves it uncovered, so the window opens
 * centred.
 */
export function placeBeside(size: FrameSize, anchor: WindowRect, workspace: Workspace, minimum: FrameSize): WindowRect | null {
  const { bounds } = workspace
  const reach = { left: bounds.left - ANCHOR_REACH, top: bounds.top - ANCHOR_REACH, width: bounds.width + 2 * ANCHOR_REACH, height: bounds.height + 2 * ANCHOR_REACH }
  if (anchor.width <= 0 || anchor.height <= 0 || !rectsOverlap(anchor, reach)) return null
  const fitted = clampFrameSize(size, minimum, bounds)
  const rightSide = { ...fitted, left: right(anchor) + ANCHOR_GAP, top: anchor.top }
  const leftSide = { ...fitted, left: anchor.left - ANCHOR_GAP - fitted.width, top: anchor.top }
  const below = { ...fitted, left: anchor.left, top: bottom(anchor) + ANCHOR_GAP }
  const above = { ...fitted, left: anchor.left, top: anchor.top - ANCHOR_GAP - fitted.height }
  const facing = bottom(anchor) <= bounds.top ? below
    : anchor.top >= bottom(bounds) ? above
      : right(anchor) <= bounds.left ? rightSide
        : anchor.left >= right(bounds) ? leftSide
          : null
  const order = [rightSide, leftSide, below, above]
  const sides = facing ? [facing, ...order.filter(side => side !== facing)] : order
  for (const side of sides) {
    const held = keepInWorkspace(side, workspace, minimum)
    if (!rectsOverlap(held, anchor)) return held
  }
  return null
}

/** A window moved by the pointer's travel since the gesture began. */
export function moveWindowRect(start: WindowRect, dx: number, dy: number, workspace: Workspace, minimum: FrameSize): WindowRect {
  return keepInWorkspace({ ...start, left: start.left + dx, top: start.top + dy }, workspace, minimum)
}

/**
 * A window resized from a handle by the pointer's travel since the gesture
 * began. Only the edges the handle holds move, so the edge in hand follows the
 * pointer and the opposite edges stay put. The edges stop at the workspace, at
 * the minimum and at an avoided zone. Returns null when no size along the
 * gesture keeps the window clear, so the caller keeps the last good one.
 */
export function resizeWindowRect(
  start: WindowRect,
  handle: HandleAxis,
  dx: number,
  dy: number,
  workspace: Workspace,
  minimum: FrameSize,
): WindowRect | null {
  const { bounds } = workspace
  let left = start.left
  let top = start.top
  let east = right(start)
  let south = bottom(start)
  if (handle.x === 1) east = clamp(east + dx, left + minimum.width, Math.max(left + minimum.width, right(bounds)))
  if (handle.x === -1) left = clamp(left + dx, Math.min(bounds.left, east - minimum.width), east - minimum.width)
  if (handle.y === 1) south = clamp(south + dy, top + minimum.height, Math.max(top + minimum.height, bottom(bounds)))
  if (handle.y === -1) top = clamp(top + dy, Math.min(bounds.top, south - minimum.height), south - minimum.height)
  let next: WindowRect = { left, top, width: east - left, height: south - top }
  for (const zone of workspace.avoid) {
    if (!rectsOverlap(next, zone)) continue
    const current = next
    const options: WindowRect[] = []
    if (handle.x === 1) options.push({ ...current, width: zone.left - current.left })
    if (handle.x === -1) options.push({ ...current, left: right(zone), width: right(current) - right(zone) })
    if (handle.y === 1) options.push({ ...current, height: zone.top - current.top })
    if (handle.y === -1) options.push({ ...current, top: bottom(zone), height: bottom(current) - bottom(zone) })
    const held = options
      .filter(option => option.width >= minimum.width && option.height >= minimum.height && !rectsOverlap(option, zone))
      .sort((a, b) => area(b) - area(a))[0]
    if (!held) return null
    next = held
  }
  return next
}
