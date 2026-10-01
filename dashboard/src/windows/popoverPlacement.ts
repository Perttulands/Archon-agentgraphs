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
 * A popover is placed by the size it opens at (`openHeight`) and then given
 * the free room next to it, up to `height`, to grow into: downward, or upward
 * when it sits above its anchor. A list inside scrolls within that room. Where
 * its full width finds no free place it narrows, down to `minWidth`. It takes
 * the nearest place that covers nothing, preferring places with room for at
 * least `minHeight`; each pixel of room or width it gives up counts as a
 * little distance, so it gives them up only to stay closer. Only a place within
 * `reach` of the anchor counts as beside it. When no place within reach is
 * free even at its opening size, it takes the place within reach that covers
 * the least, so it never wanders across the view. Placement is pure: the same
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

export interface PopoverSize {
  width: number
  /** The narrowest it may open, where its full width finds no free place; its full width when absent. */
  minWidth?: number
  /** The height it opens at, before a list opens inside it. */
  openHeight: number
  /** The most it grows to, with a list open. */
  height: number
  /** The room it prefers to have for a list; less is allowed, with the list scrolling more. */
  minHeight: number
}

export interface PopoverPlace {
  /** Where it opens; `rect.height` is the room it may grow into. */
  rect: WindowRect
  /** It sits above its anchor and grows upward, toward it. */
  growsUp: boolean
  /** Gap between the popover and its anchor, in pixels. */
  distance: number
  /** Pixels of obstacles (and of a card home) it covers; zero unless nothing within reach is free. */
  covered: number
}

/** Room left between a popover and what it opens beside. */
export const POPOVER_GAP = 8

/** How far from its anchor a popover may open. */
export const POPOVER_REACH = 160

/** How much distance one pixel of room given up is worth. */
const ROOM_COST = 0.3

/** How much distance one pixel of width given up is worth. */
const WIDTH_COST = 0.6

/** Each step a popover narrows by. */
const NARROW_STEP = 60

/** How much distance one pixel of room short of `minHeight` is worth. */
const CRAMPED_COST = 1.5

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

/** Where a popover of `size` opens in `scene`. */
export function placePopover(size: PopoverSize, scene: PopoverScene, reach = POPOVER_REACH): PopoverPlace {
  const { width: fullWidth, openHeight, height, minHeight } = size
  const minWidth = Math.min(fullWidth, size.minWidth ?? fullWidth)
  const widths: number[] = []
  for (let w = fullWidth; w > minWidth; w -= NARROW_STEP) widths.push(w)
  widths.push(minWidth)
  const width = fullWidth
  const { bounds, anchor } = scene
  const home = scene.home || null
  const near = grow(anchor, reach + Math.max(width, height))
  const obstacles = scene.obstacles.filter(rect => rect.width > 0 && rect.height > 0 && rectsOverlap(rect, near)).map(rect => grow(rect, POPOVER_GAP / 2))
  const keepAnchor = grow(anchor, POPOVER_GAP - 1)
  const walls = [keepAnchor, ...(home && !scene.homeCovers ? [grow(home, POPOVER_GAP / 2)] : []), ...obstacles]
  // A home window the popover may cover hides whatever lies under it, so only what shows counts.
  const free = home && scene.homeCovers ? home : null
  const covers = (rect: WindowRect, wall: WindowRect) => {
    const hit = intersection(rect, wall)
    if (!hit) return 0
    return hit.width * hit.height - (free && wall !== keepAnchor ? overlapArea(hit, free) : 0)
  }
  const cost = (rect: WindowRect) => walls.reduce((sum, wall) => sum + covers(rect, wall), 0)
  const fits = (rect: WindowRect) => rect.left >= bounds.left && rect.top >= bounds.top && right(rect) <= right(bounds) && bottom(rect) <= bottom(bounds)

  // What limits growth: every wall but those a covered home window hides entirely.
  const hidden = (wall: WindowRect) => Boolean(free) && wall !== keepAnchor && overlapArea(wall, free!) === wall.width * wall.height
  const growthWalls = walls.filter(wall => !hidden(wall))
  /** The room it may grow into from its opening rectangle: down, or up when it sits above its anchor. */
  const room = (open: WindowRect, up: boolean): number => {
    let limit = up ? bounds.top : bottom(bounds)
    for (const wall of growthWalls) {
      if (wall.left >= right(open) || right(wall) <= open.left) continue
      if (up && bottom(wall) <= open.top + 0.5) limit = Math.max(limit, bottom(wall))
      if (!up && wall.top >= bottom(open) - 0.5) limit = Math.min(limit, wall.top)
    }
    return Math.min(height, up ? bottom(open) - limit : limit - open.top)
  }

  const ys = new Set<number>([anchor.top, bottom(anchor) - openHeight, bottom(anchor) + POPOVER_GAP, anchor.top - POPOVER_GAP - openHeight, bounds.top, bottom(bounds) - openHeight])
  for (const wall of walls) {
    ys.add(bottom(wall) + 1)
    ys.add(wall.top - 1 - openHeight)
  }

  let best: PopoverPlace | null = null
  let bestScore = 0
  let bestRank = 0
  let fallback: PopoverPlace | null = null
  for (const w of widths) {
    const narrowed = WIDTH_COST * (fullWidth - w)
    const xs = new Set<number>([anchor.left, right(anchor) - w, right(anchor) + POPOVER_GAP, anchor.left - POPOVER_GAP - w, bounds.left, right(bounds) - w])
    for (const wall of walls) {
      xs.add(right(wall) + 1)
      xs.add(wall.left - 1 - w)
    }
    for (const x of xs) {
      for (const y of ys) {
        const open = { left: Math.round(x), top: Math.round(y), width: w, height: openHeight }
        if (!fits(open)) continue
        const distance = separation(open, anchor)
        if (distance > reach) continue
        const growsUp = bottom(open) <= anchor.top
        const covered = cost(open)
        if (covered > 0) {
          if (w === fullWidth && (!fallback || covered < fallback.covered || (covered === fallback.covered && distance < fallback.distance))) {
            fallback = { rect: open, growsUp, distance, covered }
          }
          continue
        }
        const grown = Math.max(openHeight, room(open, growsUp))
        const rect = growsUp ? { ...open, top: bottom(open) - grown, height: grown } : { ...open, height: grown }
        const score = distance + narrowed + ROOM_COST * (height - grown) + CRAMPED_COST * Math.max(0, minHeight - grown)
        const rank = sideRank(open, anchor)
        if (!best || score < bestScore - 0.5 || (Math.abs(score - bestScore) <= 0.5 && (rank < bestRank || (rank === bestRank && Math.abs(open.top - anchor.top) < Math.abs(best.rect.top - anchor.top))))) {
          best = { rect, growsUp, distance, covered: 0 }
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
    top: Math.max(bounds.top, Math.min(anchor.top, bottom(bounds) - openHeight)),
    width,
    height: openHeight,
  }
  return { rect, growsUp: false, distance: separation(rect, anchor), covered: cost(rect) }
}
