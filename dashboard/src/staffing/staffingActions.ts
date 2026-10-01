/* Every way of staffing ends in one of these: a sentence staffed, a digit for
 * the effort, the offer taken, a role dropped from the rail, or a slot's
 * staffing moved onto another. Each states the slot's staffing in full and is
 * one undo step. */
import type { SlotRef, StaffingStore } from './staffingStore'
import { roleName, withEffort, withRole, freshStaffing, type Staffing, type StaffingCatalog, type Suggestion } from './staffingModel'

export interface StaffingHost {
  catalog: StaffingCatalog
  /** Writes a slot's staffing in full (null empties it) with its undo entry; resolves to a refusal in words, or null. */
  save: (ref: SlotRef, next: Staffing | null, label: string) => Promise<string | null>
  /** Moves one slot's staffing onto another, which gives its own back, as one edit; resolves to a refusal, or null. */
  move?: (from: SlotRef, fromNext: Staffing | null, to: SlotRef, toNext: Staffing | null) => Promise<string | null>
}

export function staffingLabel(ref: SlotRef): string {
  return `the staffing of “${ref.label}”`
}

export function staff(store: StaffingStore, host: StaffingHost, ref: SlotRef, saved: Staffing | null, next: Staffing | null, meta: { note?: string; effortByHand?: boolean; offer?: Suggestion; label?: string } = {}): Promise<boolean> {
  const previous = store.current(ref.key, saved)
  return store.commit(ref, previous, next, meta, () => host.save(ref, next, meta.label || staffingLabel(ref)))
}

/** One input sets a slot's effort: a digit on a focused slot. It counts as picked by hand. */
export function setEffort(store: StaffingStore, host: StaffingHost, ref: SlotRef, saved: Staffing | null, effort: string): void {
  const current = store.current(ref.key, saved)
  if (!current) {
    store.say(ref.key, `${ref.label} is not staffed: open it to choose its agent first.`, 'refused')
    return
  }
  const outcome = withEffort(host.catalog, current, effort)
  if (outcome.refused) {
    store.say(ref.key, outcome.refused, 'refused')
    return
  }
  if (outcome.next.effort === current.effort) return
  void staff(store, host, ref, saved, outcome.next, { effortByHand: true, label: `the effort of “${ref.label}”` })
}

/** The standing offer, taken in one click: the policy's effort, with its reason. */
export function takeOffer(store: StaffingStore, host: StaffingHost, ref: SlotRef, saved: Staffing | null): void {
  const current = store.current(ref.key, saved)
  const offer = store.offer(ref.key)
  if (!current || !offer) return
  void staff(store, host, ref, saved, { ...current, effort: offer.effort }, { note: `Effort ${offer.effort}: ${offer.reason}.`, label: `the suggested effort of “${ref.label}”` })
}

/** A role dropped from the rail lands by the one rule; vanilla drops the role. An empty slot starts from its fresh staffing. */
export function dropRole(store: StaffingStore, host: StaffingHost, ref: SlotRef, saved: Staffing | null, roleId: string | null): void {
  const current = store.current(ref.key, saved)
  const base = current || freshStaffing(host.catalog, ref)
  const role = roleId ? host.catalog.roles.find(entry => entry.id === roleId) || null : null
  if (roleId && !role) {
    store.say(ref.key, `${roleId} is not a role you can staff.`, 'refused')
    return
  }
  const outcome = withRole(host.catalog, ref, base, role, store.effortByHand(ref.key, base.effort))
  if (current && outcome.next.role === current.role && outcome.next.effort === current.effort) return
  void staff(store, host, ref, saved, outcome.next, { note: outcome.note, offer: outcome.offer })
}

/** What a dropped role would make of a slot, for its preview while dragging. */
export function previewRole(store: StaffingStore, host: StaffingHost, ref: SlotRef, saved: Staffing | null, roleId: string | null): Staffing | null {
  const current = store.current(ref.key, saved)
  const base = current || freshStaffing(host.catalog, ref)
  const role = roleId ? host.catalog.roles.find(entry => entry.id === roleId) || null : null
  if (roleId && !role) return current
  return withRole(host.catalog, ref, base, role, store.effortByHand(ref.key, base.effort)).next
}

/** A wrong slot is fixed in one drag: the staffing moves, and a staffed target gives its own back. */
export async function moveStaffing(store: StaffingStore, host: StaffingHost, from: SlotRef, fromSaved: Staffing | null, to: SlotRef, toSaved: Staffing | null): Promise<boolean> {
  const moving = store.current(from.key, fromSaved)
  const displaced = store.current(to.key, toSaved)
  if (!moving || from.key === to.key || !host.move) return false
  const ok = await store.commitMove(from, displaced, to, moving, () => host.move!(from, displaced, to, moving))
  if (ok) store.say(to.key, displaced
    ? `Swapped: ${to.label} takes ${roleName(host.catalog, moving.role)}, ${from.label} takes ${roleName(host.catalog, displaced.role)}.`
    : `Moved to ${to.label}; ${from.label} is empty.`, 'note')
  return ok
}
