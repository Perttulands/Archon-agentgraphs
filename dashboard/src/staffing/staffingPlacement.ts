/* Where staffing's popovers open: the sentence window and the note that says
 * why a slot changed. Each opens right beside what it belongs to and covers
 * no card, operator note, open window or control (windows/popoverPlacement.ts). */
import type { CSSProperties } from 'react'
import { measureElement } from '../windows/cockpitScene'
import { placePopover, type PopoverPlace } from '../windows/popoverPlacement'
import type { ViewScene } from '../windows/WindowManager'
import type { WindowRect, Workspace } from '../windows/windowGeometry'

/** What a view tells staffing about itself: where popovers may go, the windows open over it, and what it shows. */
export interface StaffingStage {
  workspace: () => Workspace
  windows: () => readonly WindowRect[]
  /** Cards and operator notes (landmarks) and the rest of the view (content). */
  scene: () => ViewScene
}

const CARDS = '.formation, .missioncard, .gatecard, .toolcard, .endcard, .note-sticky'

const measured = (elements: Iterable<Element>) => [...elements].map(element => measureElement(element)).filter((rect): rect is WindowRect => rect !== null)

/** A view that does not describe itself: the viewport below the app bar, with its cards and notes. */
export const viewportStage: StaffingStage = {
  workspace: () => ({ bounds: { left: 8, top: 56, width: Math.max(0, window.innerWidth - 16), height: Math.max(0, window.innerHeight - 64) }, avoid: [] }),
  windows: () => [],
  scene: () => ({ landmarks: measured(document.querySelectorAll(CARDS)) }),
}

const contains = (outer: WindowRect, inner: WindowRect) =>
  inner.left >= outer.left && inner.top >= outer.top && inner.left + inner.width <= outer.left + outer.width && inner.top + inner.height <= outer.top + outer.height

/**
 * Where a popover of `width` by up to `height` opens beside `anchor`: a slot
 * (whose card is never covered) or a word in a window or the inspector (which
 * the popover may drop down over, as a list does). `also` names more to keep
 * clear, such as the open sentence window for a note.
 */
export function placeBeside(anchor: Element, width: number, height: number, minHeight: number, stage: StaffingStage, also: readonly WindowRect[] = []): PopoverPlace {
  const anchorRect = measureElement(anchor, true)!
  const holder = anchor.closest('.fwin, .agx-inspector')
  const card = holder ? null : anchor.closest('.formation, section.formation')
  const home = measureElement(holder || card || anchor, true)
  const workspace = stage.workspace()
  const centre = { left: anchorRect.left + anchorRect.width / 2, top: anchorRect.top + anchorRect.height / 2, width: 0, height: 0 }
  // The window the anchor sits in is its home, not an obstacle; cards it hides are not on screen.
  const windows = stage.windows().filter(rect => !contains(rect, centre))
  const landmarks = (stage.scene().landmarks || []).filter(rect => !(holder && home && contains(home, rect)))
  return placePopover(width, height, minHeight, {
    bounds: workspace.bounds,
    anchor: anchorRect,
    home,
    homeCovers: Boolean(holder),
    obstacles: [...workspace.avoid, ...windows, ...landmarks, ...also],
  })
}

/** The style that holds a placed popover; one placed above its anchor grows upward, toward it. */
export function popoverStyle(place: PopoverPlace, anchor: Element): CSSProperties {
  const { rect } = place
  const above = rect.top + rect.height <= anchor.getBoundingClientRect().top
  return above
    ? { left: rect.left, bottom: window.innerHeight - (rect.top + rect.height), width: rect.width, maxHeight: rect.height }
    : { left: rect.left, top: rect.top, width: rect.width, maxHeight: rect.height }
}
