/* The floating layer staffing draws over a view: the open sentence window and
 * the note that says why a staffing landed as it did. A note never covers a
 * slot and never takes a pointer or a drop. */
import { useEffect, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { SentenceWindow } from './SentenceWindow'
import type { StaffingHost } from './staffingActions'
import type { Staffing } from './staffingModel'
import { useStaffingVersion, type SlotRef, type Stamp, type StaffingStore } from './staffingStore'
import './staffing.css'

type Rect = { left: number; top: number; right: number; bottom: number }
const overlaps = (a: Rect, b: Rect) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom

export function slotElement(key: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`.slot[data-slot-key="${CSS.escape(key)}"]`)
}

/** Below the card, above it, then beside it: the first place that covers no slot and stays on screen. */
function stampPlace(stamp: Stamp, width: number, height: number): CSSProperties | null {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const taken = [...document.querySelectorAll<HTMLElement>('.slot[data-slot-key], .staffing-window')].map(element => element.getBoundingClientRect())
  const fits = (rect: Rect) => rect.left >= 4 && rect.top >= 52 && rect.right <= vw - 4 && rect.bottom <= vh - 4 && !taken.some(other => overlaps(rect, other))
  let anchor: Rect
  let card: Rect
  if (stamp.key) {
    const slot = slotElement(stamp.key)
    if (!slot) return null
    anchor = slot.getBoundingClientRect()
    card = (slot.closest('.formation') || slot).getBoundingClientRect()
  } else if (stamp.point) {
    anchor = { left: stamp.point.x, top: stamp.point.y, right: stamp.point.x, bottom: stamp.point.y }
    card = anchor
  } else return null
  const candidates: Rect[] = [
    { left: anchor.left, top: card.bottom + 6, right: anchor.left + width, bottom: card.bottom + 6 + height },
    { left: anchor.left, top: card.top - 6 - height, right: anchor.left + width, bottom: card.top - 6 },
    { left: card.right + 8, top: anchor.top, right: card.right + 8 + width, bottom: anchor.top + height },
    { left: card.left - 8 - width, top: anchor.top, right: card.left - 8, bottom: anchor.top + height },
  ]
  const spot = candidates.find(fits) || candidates.find(rect => rect.left >= 4 && rect.right <= vw - 4 && rect.top >= 52 && rect.bottom <= vh - 4) || candidates[0]
  return { left: spot.left, top: spot.top, width }
}

function StampView({ stamp }: { stamp: Stamp }) {
  const width = 360
  const height = stamp.text.length > 90 ? 58 : stamp.text.length > 45 ? 42 : 28
  const style = stampPlace(stamp, width, height)
  if (!style) return null
  return (
    <div className={`staffing-stamp ${stamp.tone}`} style={style} role="status" data-testid="staffing-stamp">
      <span>{stamp.text}</span>
    </div>
  )
}

export function StaffingLayer({ store, host, savedOf }: {
  store: StaffingStore
  host: StaffingHost
  /** What a slot holds in the mission now. */
  savedOf: (ref: SlotRef) => Staffing | null
}) {
  useStaffingVersion(store)
  const { open, stamp } = store
  // A note reads for as long as its words need, at most nine seconds.
  useEffect(() => {
    if (!stamp) return
    const timer = window.setTimeout(() => store.clearStamp(stamp.id), Math.min(9000, 3500 + stamp.text.length * 40))
    return () => window.clearTimeout(timer)
  }, [stamp, store])
  // A zoom or pan moves the slot out from under its window: close it.
  useEffect(() => {
    if (!open) return
    const close = (event: WheelEvent) => {
      if ((event.target as Element | null)?.closest?.('.staffing-window')) return
      store.setDraft(open.ref.key, undefined)
      store.setOpen(null)
    }
    window.addEventListener('wheel', close, { passive: true })
    return () => window.removeEventListener('wheel', close)
  }, [open, store])
  return createPortal(
    <div className="staffing-layer">
      {open ? <SentenceWindow key={`${open.ref.key}:${open.part || ''}`} store={store} host={host} open={open} saved={savedOf(open.ref)} /> : null}
      {stamp ? <StampView key={stamp.id} stamp={stamp} /> : null}
    </div>,
    document.body,
  )
}

/** The keyboard path, in the canvas corner; it gives way while a sentence is open. */
export function StaffingKeyHint({ store }: { store: StaffingStore }) {
  useStaffingVersion(store)
  if (store.open) return null
  return (
    <div className="staffing-keyhint" data-testid="staffing-keyhint">
      <kbd>N</kbd> next empty slot · <kbd>1</kbd>–<kbd>6</kbd> effort on a focused slot
    </div>
  )
}
