/* Where the staffing sentence opens: like a dropdown, on the row of the slot
 * (or the word) that was clicked. One rule on the canvas, in a node window, in
 * Flow's node windows and in the Agents inspector. */
import type { WindowRect } from '../windows/windowGeometry'

/** What a view tells staffing about itself: where the sentence may go, and how to make room for it. */
export interface StaffingStage {
  /** Where the sentence may open, in viewport pixels. */
  bounds: () => WindowRect
  /** Pans the view up (negative) or down by `dy`, resolving once it has moved; a view that cannot pan leaves it out. */
  makeRoom?: (dy: number) => Promise<void>
}

/** A view that does not describe itself: the viewport below the app bar. */
export const viewportStage: StaffingStage = {
  bounds: () => ({ left: 8, top: 56, width: Math.max(0, window.innerWidth - 16), height: Math.max(0, window.innerHeight - 64) }),
}

/** A pan that makes room for the sentence is over within this, before the sentence fades in. */
export const PAN_MS = 120

/** Room between the row and the sentence below or above it. */
export const DROP_GAP = 4

export interface DropPlace {
  left: number
  top: number
  /** Its height: the height asked for, unless neither side has that room even after a pan. */
  height: number
  above: boolean
  /** How far the view pans first, in pixels; negative moves it up. Zero when it already fits. */
  pan: number
}

/**
 * The sentence opens directly below its anchor, left-aligned with it and held
 * inside the bounds across. Where the room below is short, a view that can pan
 * moves up by exactly what is missing, so long as the anchor stays in view.
 * Otherwise it opens directly above, panning down by what is missing there if
 * it can. Where neither side can hold it, it takes the side with more room and
 * is that much shorter. It never covers its anchor.
 */
export function placeDrop(anchor: WindowRect, size: { width: number; height: number }, bounds: WindowRect, canPan: boolean): DropPlace {
  const { width, height } = size
  const left = Math.round(Math.max(bounds.left, Math.min(anchor.left, bounds.left + bounds.width - width)))
  const boundsBottom = bounds.top + bounds.height
  const anchorBottom = anchor.top + anchor.height
  const below = anchorBottom + DROP_GAP
  const roomBelow = boundsBottom - below
  const roomAbove = anchor.top - DROP_GAP - bounds.top
  if (roomBelow >= height) return { left, top: Math.round(below), height, above: false, pan: 0 }
  // The anchor may rise to the top of the bounds; the room below grows by as much.
  if (canPan && roomBelow + (anchor.top - bounds.top) >= height) {
    const pan = -Math.ceil(height - roomBelow)
    return { left, top: Math.round(below + pan), height, above: false, pan }
  }
  if (roomAbove >= height) return { left, top: Math.round(anchor.top - DROP_GAP - height), height, above: true, pan: 0 }
  if (canPan && roomAbove + (boundsBottom - anchorBottom) >= height) {
    const pan = Math.ceil(height - roomAbove)
    return { left, top: Math.round(anchor.top + pan - DROP_GAP - height), height, above: true, pan }
  }
  if (roomBelow >= roomAbove) return { left, top: Math.round(below), height: Math.max(0, Math.floor(roomBelow)), above: false, pan: 0 }
  const short = Math.max(0, Math.floor(roomAbove))
  return { left, top: Math.round(anchor.top - DROP_GAP - short), height: short, above: true, pan: 0 }
}
