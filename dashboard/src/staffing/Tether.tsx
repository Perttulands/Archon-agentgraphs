/* A tether: the short accent line that ties a popover (the sentence window or
 * a landing note) to the slot it belongs to, so it reads as that slot's own
 * wherever placement had to put it. It runs from the slot's edge that faces
 * the popover to the popover's nearest edge, with one bend when they do not
 * line up, and takes no pointer. */

type Box = { left: number; top: number; right: number; bottom: number }

const clamp = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value))

/**
 * The tether's path, in viewport pixels, from `slot` to `popover`; null when
 * they touch or overlap. Given the slot's card (`home`), a popover above or
 * below the card is reached along the card's side, so the tether never runs
 * across the card's other slots.
 */
export function tetherPath(slot: Box, popover: Box, home?: Box | null): { d: string; from: [number, number]; to: [number, number] } | null {
  const inset = 12
  const midY = (slot.top + slot.bottom) / 2
  const midX = (slot.left + slot.right) / 2
  if (popover.left >= slot.right || popover.right <= slot.left) {
    // Beside: from the slot's facing side to the popover's, bending once if the rows do not line up.
    const right = popover.left >= slot.right
    const sx = right ? slot.right : slot.left
    const ex = right ? popover.left : popover.right
    const ey = clamp(midY, popover.top + inset, popover.bottom - inset)
    if (Math.abs(ex - sx) < 2) return null
    const bend = (sx + ex) / 2
    return { d: `M${sx},${midY} H${bend} V${ey} H${ex}`, from: [sx, midY], to: [ex, ey] }
  }
  if (home && (popover.top >= home.bottom || popover.bottom <= home.top)) {
    // Beyond the card: out of the slot's side, along the card's edge, into the popover's near edge.
    const right = (popover.left + popover.right) / 2 >= midX
    const sx = right ? slot.right : slot.left
    const lane = right ? home.right + 6 : home.left - 6
    const below = popover.top >= home.bottom
    const ey = below ? popover.top : popover.bottom
    const ex = clamp(lane, popover.left + inset, popover.right - inset)
    const d = ex === lane ? `M${sx},${midY} H${lane} V${ey}` : `M${sx},${midY} H${lane} V${(below ? home.bottom : home.top) + (below ? 6 : -6)} H${ex} V${ey}`
    return { d, from: [sx, midY], to: [ex, ey] }
  }
  if (popover.top >= slot.bottom || popover.bottom <= slot.top) {
    // Below or above: straight down or up where they overlap across, else with one bend.
    const below = popover.top >= slot.bottom
    const sy = below ? slot.bottom : slot.top
    const ey = below ? popover.top : popover.bottom
    if (Math.abs(ey - sy) < 2) return null
    const low = Math.max(slot.left, popover.left) + inset
    const high = Math.min(slot.right, popover.right) - inset
    if (low <= high) return { d: `M${low},${sy} V${ey}`, from: [low, sy], to: [low, ey] }
    const ex = clamp(midX, popover.left + inset, popover.right - inset)
    const bend = (sy + ey) / 2
    return { d: `M${midX},${sy} V${bend} H${ex} V${ey}`, from: [midX, sy], to: [ex, ey] }
  }
  return null
}

export function Tether({ slot, popover, home }: { slot: Box | null; popover: Box | null; home?: Box | null }) {
  const path = slot && popover ? tetherPath(slot, popover, home) : null
  if (!path) return null
  return (
    <svg className="staffing-tether" aria-hidden="true" data-testid="staffing-tether">
      <path d={path.d} />
      <circle cx={path.from[0]} cy={path.from[1]} r={2.5} />
      <circle cx={path.to[0]} cy={path.to[1]} r={2.5} />
    </svg>
  )
}
