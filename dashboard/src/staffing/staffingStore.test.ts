import { describe, expect, it } from 'vitest'
import { dropRole, moveStaffing, setEffort, takeOffer, type StaffingHost } from './staffingActions'
import { catalog } from '../test/staffingCatalog'
import type { Staffing } from './staffingModel'
import { StaffingStore, type SlotRef } from './staffingStore'

const ref: SlotRef = { key: 'f:a', formationId: 'f', slotId: 'a', label: 'Worker 1', step: 'New formation' }
const other: SlotRef = { key: 'f:b', formationId: 'f', slotId: 'b', label: 'Worker 2', step: 'New formation' }
const vanilla: Staffing = { role: '', harness: 'claude-code', model: 'opus', effort: 'low' }

/** A host that records each write and answers with the given refusal, or none. */
function recordingHost(refusal: string | null = null) {
  const writes: Array<[string, Staffing | null]> = []
  const moves: Array<[string, Staffing | null, string, Staffing | null]> = []
  const host: StaffingHost = {
    catalog,
    save: async (slot, next) => { writes.push([slot.key, next]); return refusal },
    move: async (from, fromNext, to, toNext) => { moves.push([from.key, fromNext, to.key, toNext]); return refusal },
  }
  return { host, writes, moves }
}

const settle = () => new Promise(resolve => setTimeout(resolve, 0))

describe('the staffing store', () => {
  it('shows a write at once, replays its landing, and keeps it while the mission saves', async () => {
    const store = new StaffingStore()
    const { host, writes } = recordingHost()
    setEffort(store, host, ref, vanilla, 'xhigh')
    expect(store.shown(ref.key, vanilla)).toEqual({ ...vanilla, effort: 'xhigh' })
    expect(store.landed(ref.key)).toBe(1)
    await settle()
    expect(writes).toEqual([[ref.key, { ...vanilla, effort: 'xhigh' }]])
    // Saved: the slot shows the mission's staffing again, and the effort counts as picked by hand.
    expect(store.shown(ref.key, vanilla)).toEqual(vanilla)
    expect(store.effortByHand(ref.key, 'xhigh')).toBe(true)
  })

  it('says a refusal under the slot and shows what it held', async () => {
    const store = new StaffingStore()
    const { host } = recordingHost('slot "a" effort "max" is not one gpt-5.5 accepts')
    setEffort(store, host, ref, vanilla, 'max')
    await settle()
    expect(store.shown(ref.key, vanilla)).toEqual(vanilla)
    expect(store.stamp).toMatchObject({ key: ref.key, tone: 'refused', text: 'slot "a" effort "max" is not one gpt-5.5 accepts' })
    expect(store.landed(ref.key)).toBe(0)
  })

  it('refuses an effort the harness does not take, in words, without writing', () => {
    const store = new StaffingStore()
    const { host, writes } = recordingHost()
    setEffort(store, host, ref, vanilla, 'ultra')
    expect(writes).toEqual([])
    expect(store.stamp).toMatchObject({ tone: 'refused', text: 'Claude Code goes up to max; ultra is Codex only.' })
    setEffort(store, host, ref, null, 'low')
    expect(store.stamp?.text).toBe('Worker 1 is not staffed: open it to choose its agent first.')
  })

  it('keeps a hand-picked effort when a role lands, offers the policy, and takes the offer in one click', async () => {
    const store = new StaffingStore()
    const { host, writes } = recordingHost()
    setEffort(store, host, ref, vanilla, 'high')
    await settle()
    const saved = { ...vanilla, effort: 'high' }
    dropRole(store, host, ref, saved, 'critic-judge')
    expect(store.stamp?.text).toBe('high stays: you picked it by hand. Policy suggests xhigh: Critic Judge is review or architecture work, so xhigh.')
    await settle()
    const withRole = { ...saved, role: 'critic-judge' }
    expect(writes[writes.length - 1]).toEqual([ref.key, withRole])
    expect(store.offer(ref.key)).toEqual({ effort: 'xhigh', reason: 'Critic Judge is review or architecture work, so xhigh' })
    // A model change keeps the offer; taking it writes the policy's effort and drops it.
    takeOffer(store, host, ref, withRole)
    await settle()
    expect(writes[writes.length - 1]).toEqual([ref.key, { ...withRole, effort: 'xhigh' }])
    expect(store.offer(ref.key)).toBeUndefined()
    expect(store.recentRoles).toEqual(['critic-judge'])
  })

  it('lands a role on an empty slot from its fresh staffing, by the policy', async () => {
    const store = new StaffingStore()
    const { host, writes } = recordingHost()
    dropRole(store, host, ref, null, 'repo-scout')
    await settle()
    expect(writes).toEqual([[ref.key, { role: 'repo-scout', harness: 'claude-code', model: 'opus', effort: 'low' }]])
    expect(store.stamp?.text).toBe('Effort low: Repo Scout runs errands, so low.')
  })

  it('moves a staffing onto another slot and swaps a staffed target back, as one edit', async () => {
    const store = new StaffingStore()
    const { host, moves } = recordingHost()
    const critic = { ...vanilla, role: 'critic-judge', effort: 'xhigh' }
    await moveStaffing(store, host, ref, critic, other, vanilla)
    expect(moves).toEqual([[ref.key, vanilla, other.key, critic]])
    expect(store.stamp?.text).toBe('Swapped: Worker 2 takes Critic Judge, Worker 1 takes vanilla.')
    await moveStaffing(store, host, other, critic, ref, null)
    expect(moves[moves.length - 1]).toEqual([other.key, null, ref.key, critic])
    expect(store.stamp?.text).toBe('Moved to Worker 1; Worker 2 is empty.')
  })
})
