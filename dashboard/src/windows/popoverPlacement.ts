import { rectsOverlap, type WindowRect } from './windowGeometry'

/**
 * Where a popover opens: a small window that belongs to one thing on screen,
 * such as the staffing sentence of a slot or the note that says why a slot
 * changed. Unlike a floating window (windowPlacement.ts), it opens right
 * beside what it belongs to and covers none of what the operator reads.
 *
 * The scene, in viewport pixels:
 *
 * - `anchor`: what the popover belongs to, the slot or the word clicked. It is
 *   never covered.
 * - `home`: what holds the anchor, the slot's card or the window the word sits
 *   in. A card is never covered; a window may be (`homeCovers`), as a list
 *   drops down over the window it opened from.
 * - `obstacles`: what must stay readable: cards, operator notes, open windows,
 *   controls. Never covered.
 * - `bounds`: where the popover may go at all.
 *
 * The popover takes the nearest place that covers nothing, at its full height
 * or shorter, down to `minHeight` (its list scrolls instead); each pixel it
 * gives up counts as a little distance, so it shortens only to stay closer.
 * Only a place within `reach` of the anchor counts as beside it; when none
 * is free there, the place within reach that covers the least is taken, so
 * the popover never wanders across the view. Placement is pure: the same
 * scene always gives the same rectangle.
 */

export interface PopoverScene {
  bounds: WindowRect
  anchor: WindowRect
  home?: WindowRect | null
  /** The popover may cover its home (a window), never its anchor. */
  homeCovers?: boolean
  obstacles: readonly WindowRect[]
}

export interface PopoverPlace {
  rect: WindowRect
  /** Gap between the popover and its anchor, in pixels. */
  distance: number
  /** Pixels of obstacles (and of a card home) it covers; zero unless nothing within reach is free. */
  covered: number
}

/** Room left between a popover and what it opens beside. */
export const POPOVER_GAP = 8

/** How far from its anchor a popover may open. */
export const POPOVER_REACH = 160

/** Each step a popover shortens by when its full height finds no room. */
const SHORTEN_STEP = 40

/** How much distance one pixel of height given up is worth. */
const SHORTEN_COST = 0.3

const right = (rect: WindowRect) => rect.left + rect.width
const bottom = (rect: WindowRect) => rect.top + rect.height

function overlapArea(a: WindowRect, b: WindowRect): number {
  const width = Math.min(right(a), right(b)) - Math.max(a.left, b.left)
  const height = Math.min(bottom(a), bottom(b)) - Math.max(a.top, b.top)
  return width > 0 && height > 0 ? width * height : 0
}

function intersection(a: WindowRect, b: WindowRect): WindowRect | null {
  const left = Math.max(a.left, b.left)
  const top = Math.max(a.top, b.top)
  const width = Math.min(right(a), right(b)) - left
  const height = Math.min(bottom(a), bottom(b)) - top
  return width > 0 && height > 0 ? { left, top, width, height } : null
}

function separation(a: WindowRect, b: WindowRect): number {
  const dx = Math.max(0, b.left - right(a), a.left - right(b))
  const dy = Math.max(0, b.top - bottom(a), a.top - bottom(b))
  return Math.hypot(dx, dy)
}

function grow(rect: WindowRect, by: number): WindowRect {
  return { left: rect.left - by, top: rect.top - by, width: rect.width + 2 * by, height: rect.height + 2 * by }
}

// Among places equally near: right of the anchor, then below, left, above.
function sideRank(rect: WindowRect, anchor: WindowRect): number {
  if (rect.left >= right(anchor)) return 0
  if (rect.top >= bottom(anchor)) return 1
  if (right(rect) <= anchor.left) return 2
  return 3
}

/** Where a popover of `width` by up to `height` opens in `scene`. */
export function placePopover(width: number, height: number, minHeight: number, scene: PopoverScene, reach = POPOVER_REACH): PopoverPlace {
  const { bounds, anchor } = scene
  const home = scene.home || null
  const near = grow(anchor, reach + Math.max(width, height))
  const obstacles = scene.obstacles.filter(rect => rect.width > 0 && rect.height > 0 && rectsOverlap(rect, near)).map(rect => grow(rect, POPOVER_GAP / 2))
  const keepAnchor = grow(anchor, POPOVER_GAP - 1)
  const walls = [keepAnchor, ...(home && !scene.homeCovers ? [grow(home, POPOVER_GAP / 2)] : []), ...obstacles]
  const heights: number[] = []
  for (let h = height; h > minHeight; h -= SHORTEN_STEP) heights.push(h)
  heights.push(minHeight)

  // A home window the popover may cover hides whatever lies under it, so only what shows counts.
  const free = home && scene.homeCovers ? home : null
  const cost = (rect: WindowRect) => walls.reduce((sum, wall) => {
    const hit = intersection(rect, wall)
    if (!hit) return sum
    return sum + hit.width * hit.height - (free && wall !== keepAnchor ? overlapArea(hit, free) : 0)
  }, 0)
  const fits = (rect: WindowRect) => rect.left >= bounds.left && rect.top >= bounds.top && right(rect) <= right(bounds) && bottom(rect) <= bottom(bounds)

  let fallback: PopoverPlace | null = null
  let best: PopoverPlace | null = null
  let bestScore = 0
  let bestRank = 0
  for (const h of heights) {
    const xs = new Set<number>([anchor.left, right(anchor) - width, right(anchor) + POPOVER_GAP, anchor.left - POPOVER_GAP - width, bounds.left, right(bounds) - width])
    const ys = new Set<number>([anchor.top, bottom(anchor) - h, bottom(anchor) + POPOVER_GAP, anchor.top - POPOVER_GAP - h, bounds.top, bottom(bounds) - h])
    for (const wall of walls) {
      xs.add(right(wall) + 1)
      xs.add(wall.left - 1 - width)
      ys.add(bottom(wall) + 1)
      ys.add(wall.top - 1 - h)
    }
    const shortened = SHORTEN_COST * (height - h)
    for (const x of xs) {
      for (const y of ys) {
        const rect = { left: Math.round(x), top: Math.round(y), width, height: h }
        if (!fits(rect)) continue
        const distance = separation(rect, anchor)
        if (distance > reach) continue
        const covered = cost(rect)
        if (covered > 0) {
          if (!fallback || covered < fallback.covered || (covered === fallback.covered && distance < fallback.distance)) fallback = { rect, distance, covered }
          continue
        }
        const rank = sideRank(rect, anchor)
        const score = distance + shortened
        if (!best || score < bestScore - 0.5 || (Math.abs(score - bestScore) <= 0.5 && (rank < bestRank || (rank === bestRank && Math.abs(rect.top - anchor.top) < Math.abs(best.rect.top - anchor.top))))) {
          best = { rect, distance, covered: 0 }
          bestScore = score
          bestRank = rank
        }
      }
    }
  }
  if (best) return best
  if (fallback) return fallback
  // Nothing fits within reach at all: beside the anchor, held in bounds.
  const rect = {
    left: Math.max(bounds.left, Math.min(right(anchor) + POPOVER_GAP, right(bounds) - width)),
    top: Math.max(bounds.top, Math.min(anchor.top, bottom(bounds) - minHeight)),
    width,
    height: minHeight,
  }
  return { rect, distance: separation(rect, anchor), covered: cost(rect) }
}
