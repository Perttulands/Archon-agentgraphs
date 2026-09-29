import { clampFrameSize, type FrameSize } from './floatingWindowSize'
import { rectsOverlap, type WindowRect, type Workspace } from './windowGeometry'

/**
 * Where a newly opened floating window goes.
 *
 * A window opens in the free space nearest to what it belongs to, so the
 * operator can keep reading the step, its neighbours and the next link while
 * the window is open, and several windows sit side by side or cascade. The
 * view describes what it shows as a scene of rectangles, in viewport pixels,
 * each with how much it matters to keep it visible:
 *
 * - `anchor`: what the window belongs to, such as the card, the Flow row title
 *   or the chip that opened it. It stays uncovered whenever any placement
 *   allows it.
 * - `keepClear`: what the operator reads next to the window and must be able
 *   to click: the node's title, its immediate neighbours and the links to
 *   them. Like the anchor, it stays uncovered whenever any placement allows.
 * - `windows`: the windows already open. A new window keeps their title bars
 *   visible, prefers not to cover their bodies, and never opens exactly on top
 *   of one; when there is no room it cascades from them.
 * - `landmarks`: what the operator finds their way by anywhere in the view:
 *   cards and notes on the canvas, each Flow row's number, title, labels and
 *   links.
 *   Avoided more than other content, so the next thing to click stays in view.
 * - `content`: everything else the view shows (wire labels, the text of Flow
 *   rows). The window prefers empty space, such as Flow's gutters or the
 *   canvas above and below the graph, and covers content only when that is the
 *   only way to stay near the anchor at a readable size.
 *
 * To keep a particular node visible, say the step a gate passes to, put its
 * box in `keepClear`:
 *
 *   placeOpeningWindow(size, minimum, { workspace, anchor: gateBox, keepClear: [downstreamBox] })
 *
 * or, through a window component, `keepClear={() => nodeBoxes([downstreamId])}`
 * (see cockpitScene.ts).
 *
 * The window may open smaller than asked to fit free space, but never below
 * half its size on either side, nor below the kind's minimum. Its remembered
 * size is not changed. Placement is pure: the same scene always gives the same
 * rectangle.
 */

export interface PlacementScene {
  workspace: Workspace
  /** What the window belongs to. Never covered when a placement can avoid it. */
  anchor?: WindowRect | null
  /**
   * What must stay visible and clickable while the window is open: the node's
   * title, its immediate neighbours, its next link, or a downstream node the
   * window's content talks about. Strongly avoided.
   */
  keepClear?: readonly WindowRect[]
  /** The windows already open. */
  windows?: readonly WindowRect[]
  /** Cards, notes, and Flow row titles and links anywhere in the view. */
  landmarks?: readonly WindowRect[]
  /** The rest of the view's content. Weakly avoided, so free space wins. */
  content?: readonly WindowRect[]
}

/** Room left between a window and what it opens beside. */
export const PLACEMENT_GAP = 12

/** The strip of an open window's top edge, its title bar, that a new window leaves visible. */
export const TITLE_STRIP = 40

/** How far a cascaded window steps right from the one under it; it steps down by TITLE_STRIP. */
export const CASCADE_OFFSET = 28

/** How far outside the workspace an anchor still counts as in view, such as a chip in the run bar or a toolbar button above it. */
export const ANCHOR_REACH = 96

/** The smallest share of its asked size a window shrinks to when fitting free space. */
export const FIT_SHARE = 0.5

// A place is judged first by what it must not cover: avoided zones, the
// anchor, what is kept clear and the title bars of open windows. Only among
// places that cover the least of those do the preferences below decide.
// Both are covered pixels times weight.
const MUST = { avoid: 1e6, anchor: 4, keepClear: 2, title: 2 }
const PREFER = { landmark: 8, window: 4, content: 1 }
// A window keeps this much room around its anchor, and a little around what it keeps clear.
const BREATHING = { anchor: 12, keepClear: 4 }
// Each pixel of window given up to fit free space costs less than a covered pixel of content.
const SHRINK = 0.4
// Each pixel between the window and its anchor costs this much, so the window stays near.
const NEAR = 40
// Among places equally near, the one whose centre is nearest the anchor's wins;
// without an anchor, the one nearest the workspace's centre.
const CENTRED = 1
// Opening within this of an open window's top left corner hides that window.
const STACK_REACH = 16
const STACKED = 1e9

const right = (rect: WindowRect) => rect.left + rect.width
const bottom = (rect: WindowRect) => rect.top + rect.height

function overlapArea(a: WindowRect, b: WindowRect): number {
  const width = Math.min(right(a), right(b)) - Math.max(a.left, b.left)
  const height = Math.min(bottom(a), bottom(b)) - Math.max(a.top, b.top)
  return width > 0 && height > 0 ? width * height : 0
}

/** The gap between two rectangles along the axis that separates them; zero when they touch or overlap. */
function separation(a: WindowRect, b: WindowRect): number {
  const dx = Math.max(0, b.left - right(a), a.left - right(b))
  const dy = Math.max(0, b.top - bottom(a), a.top - bottom(b))
  return Math.hypot(dx, dy)
}

function centreDistance(a: WindowRect, b: WindowRect): number {
  return Math.hypot(a.left + a.width / 2 - b.left - b.width / 2, a.top + a.height / 2 - b.top - b.height / 2)
}

function grow(rect: WindowRect, by: number): WindowRect {
  return { left: rect.left - by, top: rect.top - by, width: rect.width + 2 * by, height: rect.height + 2 * by }
}

function usable(rect: WindowRect | null | undefined): rect is WindowRect {
  return Boolean(rect) && rect!.width > 0 && rect!.height > 0 && Number.isFinite(rect!.left) && Number.isFinite(rect!.top)
}

function titleStrip(window: WindowRect): WindowRect {
  return { ...window, height: Math.min(window.height, TITLE_STRIP) }
}

// Mirroring the scene lets one routine grow a window right and down from a
// corner, and serve the other three directions too.
type Mirror = { x: boolean; y: boolean }

function mirror(rect: WindowRect, flip: Mirror): WindowRect {
  return {
    left: flip.x ? -right(rect) : rect.left,
    top: flip.y ? -bottom(rect) : rect.top,
    width: rect.width,
    height: rect.height,
  }
}

/**
 * The largest window up to `size` that grows right and down from (x, y)
 * without meeting an obstacle or leaving the bounds, tried width first and
 * height first.
 */
function growFrom(x: number, y: number, size: FrameSize, bounds: WindowRect, obstacles: readonly WindowRect[]): WindowRect[] {
  if (x < bounds.left || y < bounds.top || x >= right(bounds) || y >= bottom(bounds)) return []
  if (obstacles.some(zone => x >= zone.left && x < right(zone) && y >= zone.top && y < bottom(zone))) return []
  const widthAt = (top: number, height: number) => obstacles.reduce((width, zone) => (
    zone.left >= x && zone.top < top + height && bottom(zone) > top ? Math.min(width, zone.left - x) : width
  ), Math.min(size.width, right(bounds) - x))
  const heightAt = (left: number, width: number) => obstacles.reduce((height, zone) => (
    zone.top >= y && zone.left < left + width && right(zone) > left ? Math.min(height, zone.top - y) : height
  ), Math.min(size.height, bottom(bounds) - y))
  const wide = widthAt(y, 1)
  const deep = heightAt(x, 1)
  return [
    { left: x, top: y, width: wide, height: heightAt(x, wide) },
    { left: x, top: y, width: widthAt(y, deep), height: deep },
  ].filter(rect => rect.width > 0 && rect.height > 0)
}

const clamp = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value))

interface Plan {
  candidates: WindowRect[]
  violation: (rect: WindowRect) => number
  cost: (rect: WindowRect) => number
}

/** Where a new window of `size` opens in `scene`. */
export function placeOpeningWindow(size: FrameSize, minimum: FrameSize, scene: PlacementScene): WindowRect {
  const { candidates, violation, cost } = plan(size, minimum, scene)
  let best = candidates[0]
  let bestViolation = Number.POSITIVE_INFINITY
  let bestCost = Number.POSITIVE_INFINITY
  for (const candidate of candidates) {
    const broken = violation(candidate)
    if (broken > bestViolation) continue
    const value = cost(candidate)
    if (broken < bestViolation || value < bestCost) {
      best = candidate
      bestViolation = broken
      bestCost = value
    }
  }
  return {
    left: Math.round(best.left),
    top: Math.round(best.top),
    width: Math.round(best.width),
    height: Math.round(best.height),
  }
}

/**
 * How many places placing a window in `scene` weighs. It depends only on what
 * lies within reach of the workspace, so a long Flow scrolled mostly out of
 * view costs no more than a short one.
 */
export function placementCandidateCount(size: FrameSize, minimum: FrameSize, scene: PlacementScene): number {
  return plan(size, minimum, scene).candidates.length
}

function plan(size: FrameSize, minimum: FrameSize, scene: PlacementScene): Plan {
  const { bounds } = scene.workspace
  // Only what comes within the gap of the workspace can shape a place in it.
  const near = grow(bounds, PLACEMENT_GAP)
  const inReach = (rect: WindowRect | null | undefined): rect is WindowRect => usable(rect) && rectsOverlap(rect, near)
  const avoid = scene.workspace.avoid.filter(inReach)
  const full = clampFrameSize(size, minimum, bounds)
  const smallest = {
    width: Math.min(full.width, Math.max(minimum.width, Math.ceil(full.width * FIT_SHARE))),
    height: Math.min(full.height, Math.max(minimum.height, Math.ceil(full.height * FIT_SHARE))),
  }
  const reach = grow(bounds, ANCHOR_REACH)
  // An anchor scrolled out of view says nothing about where to open.
  const anchor = usable(scene.anchor) && rectsOverlap(scene.anchor, reach) ? scene.anchor : null
  const keepClear = (scene.keepClear || []).filter(inReach)
  const windows = (scene.windows || []).filter(inReach)
  const landmarks = (scene.landmarks || []).filter(inReach)
  const content = (scene.content || []).filter(inReach)
  const centre = {
    left: bounds.left + (bounds.width - full.width) / 2,
    top: bounds.top + (bounds.height - full.height) / 2,
    width: full.width,
    height: full.height,
  }

  const must: Array<[WindowRect, number]> = [
    ...avoid.map(rect => [rect, MUST.avoid] as [WindowRect, number]),
    ...(anchor ? [[grow(anchor, BREATHING.anchor), MUST.anchor] as [WindowRect, number]] : []),
    ...keepClear.map(rect => [grow(rect, BREATHING.keepClear), MUST.keepClear] as [WindowRect, number]),
    ...windows.map(rect => [titleStrip(rect), MUST.title] as [WindowRect, number]),
  ]
  const prefer: Array<[WindowRect, number]> = [
    ...landmarks.map(rect => [rect, PREFER.landmark] as [WindowRect, number]),
    ...windows.map(rect => [rect, PREFER.window] as [WindowRect, number]),
    ...content.map(rect => [rect, PREFER.content] as [WindowRect, number]),
  ]
  const covered = (rect: WindowRect, zones: Array<[WindowRect, number]>) => zones.reduce((total, [zone, weight]) => total + weight * overlapArea(rect, zone), 0)

  const violation = (rect: WindowRect): number => {
    let total = covered(rect, must)
    for (const open of windows) {
      if (Math.abs(open.left - rect.left) < STACK_REACH && Math.abs(open.top - rect.top) < STACK_REACH) total += STACKED
    }
    return total
  }
  const cost = (rect: WindowRect): number => covered(rect, prefer)
    + SHRINK * (full.width * full.height - rect.width * rect.height)
    + (anchor ? NEAR * separation(rect, anchor) + CENTRED * centreDistance(rect, anchor) : CENTRED * centreDistance(rect, centre))

  // Free space is measured between everything in the scene, each with a
  // margin; and, for a window that has to cover some content, between only
  // what it must not cover.
  const margin = (rect: WindowRect) => grow(rect, PLACEMENT_GAP)
  const mustClear = [...(anchor ? [anchor] : []), ...keepClear, ...windows.map(titleStrip)].map(margin)
  const hard = [...mustClear, ...windows.map(margin), ...landmarks.map(margin), ...content.map(margin), ...avoid]
  const lenient = [...mustClear, ...avoid]

  // Edges a window can line up with, inside the workspace; an edge beyond it
  // gives only a place already held at the workspace's own edge.
  const xs = new Set<number>([bounds.left, right(bounds)])
  const ys = new Set<number>([bounds.top, bottom(bounds)])
  const addX = (x: number) => { if (x > bounds.left && x < right(bounds)) xs.add(Math.round(x)) }
  const addY = (y: number) => { if (y > bounds.top && y < bottom(bounds)) ys.add(Math.round(y)) }
  for (const zone of hard) {
    addX(zone.left)
    addX(right(zone))
    addY(zone.top)
    addY(bottom(zone))
  }
  // Lined up with the anchor's left or top edge.
  if (anchor) {
    addX(anchor.left)
    addY(anchor.top)
  }

  const candidates: WindowRect[] = []
  const hold = (rect: WindowRect): WindowRect => ({
    ...rect,
    left: clamp(rect.left, bounds.left, right(bounds) - rect.width),
    top: clamp(rect.top, bounds.top, bottom(bounds) - rect.height),
  })

  // Full size, with a corner at each edge in the scene, facing each way.
  for (const x of xs) {
    for (const y of ys) {
      candidates.push(hold({ ...full, left: x, top: y }))
      candidates.push(hold({ ...full, left: x - full.width, top: y }))
      candidates.push(hold({ ...full, left: x, top: y - full.height }))
      candidates.push(hold({ ...full, left: x - full.width, top: y - full.height }))
    }
  }
  // Beside the anchor, lined up with it; centred without one.
  if (anchor) {
    for (const top of [anchor.top, bottom(anchor) - full.height]) {
      candidates.push(hold({ ...full, left: right(anchor) + PLACEMENT_GAP, top }))
      candidates.push(hold({ ...full, left: anchor.left - PLACEMENT_GAP - full.width, top }))
    }
    candidates.push(hold({ ...full, left: anchor.left, top: bottom(anchor) + PLACEMENT_GAP }))
    candidates.push(hold({ ...full, left: anchor.left, top: anchor.top - PLACEMENT_GAP - full.height }))
  } else {
    candidates.push(hold(centre))
  }
  // Cascaded from each open window, its title bar left showing.
  for (const open of windows) {
    candidates.push(hold({ ...full, left: open.left + CASCADE_OFFSET, top: open.top + TITLE_STRIP }))
    candidates.push(hold({ ...full, left: right(open) - full.width - CASCADE_OFFSET, top: open.top + TITLE_STRIP }))
  }
  // Fitted into free space: grown from each corner in each direction, down to the smallest readable size.
  for (const obstacles of [hard, lenient]) {
    for (const flip of [{ x: false, y: false }, { x: true, y: false }, { x: false, y: true }, { x: true, y: true }]) {
      const flippedBounds = mirror(bounds, flip)
      const flipped = obstacles.map(zone => mirror(zone, flip))
      for (const x of xs) {
        for (const y of ys) {
          for (const grown of growFrom(flip.x ? -x : x, flip.y ? -y : y, full, flippedBounds, flipped)) {
            if (grown.width < smallest.width || grown.height < smallest.height) continue
            candidates.push(mirror(grown, flip))
          }
        }
      }
    }
  }

  return { candidates, violation, cost }
}
