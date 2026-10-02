/* A slot on the canvas, staffed where it lives: a click opens its sentence (a
 * word of a staffed slot opens only that word), Enter does the same from the
 * keyboard, and a digit 1-6 on a focused slot sets its effort in one input.
 * Dragging a staffed slot onto another moves its staffing there. */
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from 'react'
import type { FormationSlot } from '../components/formationsTypes'
import { staffingSentence } from '../nodeWindow/staffing'
import { SlotCaption, SlotFace, type Part } from './SlotFace'
import { setEffort, takeOffer, type StaffingHost } from './staffingActions'
import { allEfforts, offCatalog, roleName, roleTrouble, type Staffing } from './staffingModel'
import { useStaffingVersion, type SlotRef, type StaffingStore } from './staffingStore'

/** A slot whose role is retired or gone does not run until it has another role or none. */
export function RoleTrouble({ role, trouble }: { role: string; trouble: 'retired' | 'missing' }) {
  return (
    <span className="slot-warn" title={`${role} is ${trouble === 'retired' ? 'retired' : 'no longer a role'}: this slot does not run until it has another role or none.`}>
      {trouble === 'retired' ? 'role retired' : 'role missing'}
    </span>
  )
}

export function CanvasSlot({ store, host, slotRef, slot, saved, badge, classes, onGrab, onMenu }: {
  store: StaffingStore
  host: StaffingHost
  slotRef: SlotRef
  slot: FormationSlot
  /** What the slot holds in the mission. */
  saved: Staffing | null
  badge?: number
  /** The run and drop states the cockpit marks on the slot. */
  classes: string[]
  /** Starts the cockpit's pointer interaction: a drag moves the staffing, a click comes back as a sentence. */
  onGrab: (event: ReactPointerEvent<HTMLElement>, ref: SlotRef, part: Part | null) => void
  onMenu: (event: ReactMouseEvent<HTMLElement>) => void
}) {
  useStaffingVersion(store)
  const key = slotRef.key
  const current = store.current(key, saved)
  const shown = store.shown(key, saved)
  const drafting = store.drafting(key)
  const offer = store.offer(key)
  const open = store.open?.ref.key === key
  const names = (id: string) => roleName(host.catalog, id)
  const onKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (event.target !== event.currentTarget) return
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      store.setOpen({ ref: slotRef, part: null, anchor: event.currentTarget })
      return
    }
    const efforts = allEfforts(host.catalog)
    const digit = Number(event.key)
    if (digit >= 1 && digit <= efforts.length && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault()
      setEffort(store, host, slotRef, saved, efforts[digit - 1])
    }
  }
  // While its landing note shows, the slot is marked as the one the note speaks of.
  const note = store.stamp?.key === key ? store.stamp : null
  const noted = Boolean(note)
  const all = ['slot', 'staffable', shown ? 'filled' : 'empty', slot.controller ? 'ctrl' : '', open ? 'staffing-open' : '', noted ? 'staffing-noted' : '', ...classes]
  const slotWords = shown ? staffingSentence({ ...slot, agentId: shown.role, harness: shown.harness, model: shown.model, effort: shown.effort }, names) : `${slot.label}: not staffed`
  return (
    <div
      className={all.filter(Boolean).join(' ')}
      data-fid={slotRef.formationId}
      data-sid={slotRef.slotId}
      data-slot-key={key}
      data-testid={`slot-${slotRef.formationId}-${slotRef.slotId}`}
      tabIndex={0}
      role="button"
      aria-label={`Staff ${slotWords}`}
      title={shown ? slotWords : `${slot.label}: open slot. Click to staff it, or drag a role here.`}
      onPointerDown={event => {
        const part = ((event.target as Element).closest('[data-part]') as HTMLElement | null)?.dataset.part as Part | undefined
        onGrab(event, slotRef, part || null)
      }}
      onKeyDown={onKeyDown}
      onContextMenu={onMenu}
    >
      <SlotFace label={slot.label} badge={badge} staffing={shown} landed={store.landed(key)} note={note}
        marks={<>
          {shown && offCatalog(host.catalog, shown) ? <span className="slot-warn" title={`${shown.model}: not in the catalog; the harness decides.`}>model not in catalog</span> : null}
          {shown && roleTrouble(host.catalog, shown.role) ? <RoleTrouble role={names(shown.role)} trouble={roleTrouble(host.catalog, shown.role)!} /> : null}
          {offer && current ? (
            <button
              type="button"
              className="slot-offer"
              data-testid="staffing-offer"
              title={`Policy suggests ${offer.effort}: ${offer.reason}. The slot keeps ${current.effort} until you take it.`}
              onPointerDown={event => event.stopPropagation()}
              onClick={event => { event.stopPropagation(); takeOffer(store, host, slotRef, saved) }}
            >use {offer.effort}?</button>
          ) : null}
        </>}
        caption={<SlotCaption shown={shown} saved={current} drafting={drafting} landed={store.landed(key)} roleName={names} onPart={current ? () => undefined : undefined} />} />
    </div>
  )
}
