/* The floating layer staffing draws over a view: the open sentence window and
 * the note at a point where a drop missed. A note about a slot is a line in
 * the slot itself (SlotFace). A note never takes a pointer or a drop. */
import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { SentenceWindow } from './SentenceWindow'
import { viewportStage, type StaffingStage } from './staffingPlacement'
import type { StaffingHost } from './staffingActions'
import type { Staffing } from './staffingModel'
import { useStaffingVersion, type SlotRef, type Stamp, type StaffingStore } from './staffingStore'
import './staffing.css'

const STAMP_WIDTH = 360

/** A note about a drop that missed, beside the point where it was dropped. */
function PointStamp({ stamp, point }: { stamp: Stamp; point: { x: number; y: number } }) {
  const height = stamp.text.length > 45 ? 42 : 28
  const left = Math.max(4, Math.min(point.x + 12, window.innerWidth - STAMP_WIDTH - 4))
  const top = Math.max(52, Math.min(point.y + 12, window.innerHeight - height - 4))
  return (
    <div className={`staffing-stamp ${stamp.tone}`} style={{ left, top, width: STAMP_WIDTH }} role="status" data-testid="staffing-stamp">
      <span>{stamp.text}</span>
    </div>
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
      {stamp && !stamp.key && stamp.point ? <PointStamp key={stamp.id} stamp={stamp} point={stamp.point} /> : null}
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
