import type { WindowRect, Workspace } from './windowGeometry'

/**
 * Where the cockpit holds its floating windows: over the canvas, clear of the
 * zoom column and of the run bar should it ever sit on the canvas. Another
 * control that windows must never cover can say so with data-window-avoid.
 */

const INSET = 8
const GAP = 8
const AVOID = '.zoomctl, .zoomlevel, .run-banner, [data-window-avoid]'

function box(element: Element, grow: number): WindowRect {
  const rect = element.getBoundingClientRect()
  return { left: rect.left - grow, top: rect.top - grow, width: rect.width + 2 * grow, height: rect.height + 2 * grow }
}

export function cockpitWorkspace(canvas: HTMLElement | null): Workspace | null {
  if (!canvas) return null
  const area = box(canvas, -INSET)
  if (area.width <= 0 || area.height <= 0) return null
  const view = canvas.closest('.fmx') || canvas
  const avoid = [...view.querySelectorAll(AVOID)]
    .map(element => box(element, GAP))
    .filter(zone => zone.width > 2 * GAP && zone.height > 2 * GAP)
    .filter(zone => zone.left < area.left + area.width && zone.left + zone.width > area.left
      && zone.top < area.top + area.height && zone.top + zone.height > area.top)
  return { bounds: area, avoid }
}
