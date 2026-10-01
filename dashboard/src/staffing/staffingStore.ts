/* What staffing shows while it happens: the sentence window that is open, the
 * draft a slot previews, the staffing still being saved, the landing to
 * replay, the efforts picked by hand, the policy's standing offers and the
 * note under a slot. The mission holds what each slot runs; this store holds
 * only what the cockpit shows on the way there, so a slot re-renders on a
 * keystroke without the whole cockpit. */
import { useSyncExternalStore } from 'react'
import type { Part } from './SlotFace'
import { sameStaffing, type Staffing, type Suggestion } from './staffingModel'

/** A slot as staffing names it: its formation and slot IDs, and the words the policy reads. */
export interface SlotRef {
  key: string
  formationId: string
  slotId: string
  label: string
  /** The formation's title: the step the policy reads when no role says more. */
  step: string
}

export function slotKey(formationId: string, slotId: string): string {
  return `${formationId}:${slotId}`
}

export interface OpenSentence {
  ref: SlotRef
  /** One word's list, for a quick edit of a staffed slot; null opens the whole sentence. */
  part: Part | null
  /** What the window opens beside: the slot on the canvas, or the sentence in a node window or inspector. */
  anchor: Element
}

export interface Stamp {
  /** The slot the note is about, or none for a note at a point (a drop that missed). */
  key: string | null
  text: string
  tone: 'note' | 'refused'
  point?: { x: number; y: number }
  id: number
}

/** What a write needs to know besides the staffing: why, for the note, and whether the effort was picked by hand. */
export interface CommitMeta {
  note?: string
  /** The effort was chosen by hand in the cockpit, so a role landing later keeps it. */
  effortByHand?: boolean
  /** The policy's suggestion, offered on the slot when a role landed on a hand-picked effort. */
  offer?: Suggestion
  /** The undo entry's words, such as "the effort of “Worker 1”". */
  label?: string
}

export class StaffingStore {
  private version = 0
  private listeners = new Set<() => void>()
  open: OpenSentence | null = null
  private drafts = new Map<string, Staffing | null>()
  private pending = new Map<string, Staffing | null>()
  private landings = new Map<string, number>()
  private handPicked = new Map<string, string>()
  private offers = new Map<string, Suggestion>()
  stamp: Stamp | null = null
  /** The empty slot N reached last, so N moves on even after Esc closed what it opened. */
  lastNext: string | null = null
  /** The roles staffed lately, newest first: the role list offers them first. */
  recentRoles: string[] = []
  private landingCount = 0
  private stampCount = 0

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  getVersion = () => this.version

  private emit() {
    this.version++
    for (const listener of [...this.listeners]) listener()
  }

  /** What a slot shows: the draft being composed, else the staffing being saved, else what it holds. */
  shown(key: string, saved: Staffing | null): Staffing | null {
    if (this.drafts.has(key)) return this.drafts.get(key) ?? null
    if (this.pending.has(key)) return this.pending.get(key) ?? null
    return saved
  }

  /** What the slot holds as far as the cockpit knows: the staffing being saved, else the mission's. */
  current(key: string, saved: Staffing | null): Staffing | null {
    return this.pending.has(key) ? this.pending.get(key) ?? null : saved
  }

  drafting(key: string): boolean {
    return this.drafts.has(key)
  }

  landed(key: string): number {
    return this.landings.get(key) || 0
  }

  offer(key: string): Suggestion | undefined {
    return this.offers.get(key)
  }

  /** Whether the slot's effort is one he picked by hand in the cockpit. */
  effortByHand(key: string, effort: string): boolean {
    return Boolean(effort) && this.handPicked.get(key) === effort
  }

  setOpen(open: OpenSentence | null) {
    const previous = this.open
    if (previous && previous.ref.key !== open?.ref.key) this.drafts.delete(previous.ref.key)
    // Nothing stale is drawn over a window that opens.
    if (open) this.stamp = null
    this.open = open
    this.emit()
  }

  setDraft(key: string, draft: Staffing | null | undefined) {
    if (draft === undefined) {
      if (!this.drafts.has(key)) return
      this.drafts.delete(key)
    } else {
      if (this.drafts.has(key) && sameStaffing(this.drafts.get(key), draft)) return
      this.drafts.set(key, draft)
    }
    this.emit()
  }

  /**
   * A staffing is written: the slot shows it at once and replays its landing,
   * and the note says why. `save` resolves false when the mission refused it;
   * then the slot shows what it held, and the refusal is said under it.
   */
  async commit(ref: SlotRef, previous: Staffing | null, next: Staffing | null, meta: CommitMeta, save: () => Promise<string | null>): Promise<boolean> {
    const key = ref.key
    this.drafts.delete(key)
    this.pending.set(key, next)
    this.landings.set(key, ++this.landingCount)
    if (next && meta.effortByHand) this.handPicked.set(key, next.effort)
    if (next?.role) this.recentRoles = [next.role, ...this.recentRoles.filter(role => role !== next.role)].slice(0, 6)
    // A standing offer goes only when he takes it, or the role or effort changes.
    if (meta.offer) this.offers.set(key, meta.offer)
    else if (!previous || !next || previous.role !== next.role || previous.effort !== next.effort) this.offers.delete(key)
    this.stamp = meta.note ? { key, text: meta.note, tone: 'note', id: ++this.stampCount } : null
    this.emit()
    const refused = await save()
    this.pending.delete(key)
    if (refused) {
      this.landings.delete(key)
      this.offers.delete(key)
      this.stamp = { key, text: refused, tone: 'refused', id: ++this.stampCount }
    }
    this.emit()
    return !refused
  }

  /** A move writes two slots as one edit; both show the result at once. */
  async commitMove(from: SlotRef, fromNext: Staffing | null, to: SlotRef, toNext: Staffing | null, save: () => Promise<string | null>): Promise<boolean> {
    for (const [ref, next] of [[from, fromNext], [to, toNext]] as const) {
      this.drafts.delete(ref.key)
      this.pending.set(ref.key, next)
      this.landings.set(ref.key, ++this.landingCount)
      this.offers.delete(ref.key)
      this.handPicked.delete(ref.key)
    }
    this.stamp = null
    this.emit()
    const refused = await save()
    this.pending.delete(from.key)
    this.pending.delete(to.key)
    if (refused) this.stamp = { key: to.key, text: refused, tone: 'refused', id: ++this.stampCount }
    this.emit()
    return !refused
  }

  say(key: string | null, text: string, tone: Stamp['tone'], point?: Stamp['point']) {
    this.stamp = { key, text, tone, point, id: ++this.stampCount }
    this.emit()
  }

  clearStamp(id: number) {
    if (this.stamp?.id !== id) return
    this.stamp = null
    this.emit()
  }
}

export function useStaffingVersion(store: StaffingStore): number {
  return useSyncExternalStore(store.subscribe, store.getVersion)
}
