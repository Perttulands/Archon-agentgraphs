/* The floating layer staffing draws over a view: the open sentence window and
 * the note that says why a staffing landed as it did. A note never covers a
 * slot and never takes a pointer or a drop. */
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { SentenceWindow } from './SentenceWindow'
import { placeBeside, viewportStage, type StaffingStage } from './staffingPlacement'
import { Tether } from './Tether'
import type { StaffingHost } from './staffingActions'
import type { Staffing } from './staffingModel'
import { useStaffingVersion, type SlotRef, type Stamp, type StaffingStore } from './staffingStore'
import './staffing.css'

export function slotElement(key: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`.slot[data-slot-key="${CSS.escape(key)}"]`)
}

const STAMP_WIDTH = 360

/**
 * Beside the slot, the way the sentence window is placed: clear of every card,
 * operator note, open window and a sentence still open. Measured once the
 * view has settled, so a sentence that closed as it staffed the slot is gone
 * and does not push the note away. A note about a drop that missed sits beside
 * the point where it was dropped.
 */
function stampPlace(stamp: Stamp, height: number, stage: StaffingStage): CSSProperties | null {
  const sentence = [...document.querySelectorAll('.staffing-window')].map(element => element.getBoundingClientRect())
    .map(box => ({ left: box.left, top: box.top, width: box.width, height: box.height }))
  if (stamp.key) {
    const slot = slotElement(stamp.key)
    if (!slot) return null
    const place = placeBeside(slot, { width: STAMP_WIDTH, openHeight: height, height, minHeight: height }, stage, sentence)
    return { left: place.rect.left, top: place.rect.top, width: STAMP_WIDTH }
  }
  if (!stamp.point) return null
  const left = Math.max(4, Math.min(stamp.point.x + 12, window.innerWidth - STAMP_WIDTH - 4))
  const top = Math.max(52, Math.min(stamp.point.y + 12, window.innerHeight - height - 4))
  return { left, top, width: STAMP_WIDTH }
}

type Box = { left: number; top: number; right: number; bottom: number }
const boxOf = (element: Element): Box => { const r = element.getBoundingClientRect(); return { left: r.left, top: r.top, right: r.right, bottom: r.bottom } }

function StampView({ stamp, stage }: { stamp: Stamp; stage: StaffingStage }) {
  const height = stamp.text.length > 90 ? 58 : stamp.text.length > 45 ? 42 : 28
  const [style, setStyle] = useState<CSSProperties | null>(null)
  const [tether, setTether] = useState<{ slot: Box; popover: Box } | null>(null)
  const ref = useRef<HTMLDivElement | null>(null)
  // After this render commits: a sentence that closed with the landing is out of the DOM by then.
  useLayoutEffect(() => setStyle(stampPlace(stamp, height, stage)), []) // eslint-disable-line react-hooks/exhaustive-deps
  // Once placed, a tether ties the note to its slot.
  useLayoutEffect(() => {
    const slot = stamp.key ? slotElement(stamp.key) : null
    if (style && slot && ref.current) setTether({ slot: boxOf(slot), popover: boxOf(ref.current) })
  }, [style]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!style) return null
  return (
    <>
      <Tether slot={tether?.slot ?? null} popover={tether?.popover ?? null} />
      <div ref={ref} className={`staffing-stamp ${stamp.tone}`} style={style} role="status" data-testid="staffing-stamp">
        <span>{stamp.text}</span>
      </div>
    </>
  )
}

export function StaffingLayer({ store, host, savedOf, stage = viewportStage }: {
  store: StaffingStore
  host: StaffingHost
  /** What a slot holds in the mission now. */
  savedOf: (ref: SlotRef) => Staffing | null
  /** Where the sentence window may open; the viewport when the view does not say. */
  stage?: StaffingStage
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
      {open ? <SentenceWindow key={`${open.ref.key}:${open.part || ''}`} store={store} host={host} open={open} saved={savedOf(open.ref)} stage={stage} /> : null}
      {stamp ? <StampView key={stamp.id} stamp={stamp} stage={stage} /> : null}
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
