import type { FormationSlot } from '../components/formationsTypes'
import { captionText, staffingOf } from '../staffing/staffingModel'

/** Whether a slot names anything to run: a role, harness, model or effort. */
export function slotStaffed(slot: FormationSlot): boolean {
  return staffingOf(slot) !== null
}

/** "Worker 1", or "Lead (controller)"; a slot called Controller says so once. */
export function slotTitle(slot: FormationSlot): string {
  const label = slot.label || slot.id
  return `${label}${slot.controller && !/controller/i.test(label) ? ' (controller)' : ''}`
}

/**
 * "Worker 1 is vanilla on Claude Code · opus · low." A slot owns its harness,
 * model and effort; a slot without a role is a vanilla agent.
 */
export function staffingSentence(slot: FormationSlot, roleName: (id: string) => string): string {
  const staffing = staffingOf(slot)
  if (!staffing) return `${slotTitle(slot)} is not staffed.`
  return `${slotTitle(slot)} is ${staffing.role ? roleName(staffing.role) : 'vanilla'} on ${captionText(staffing)}.`
}

/** A slot in words, for its tooltip: what it runs, or how to staff an open slot. */
export function slotTooltip(slot: FormationSlot, roleName: (id: string) => string): string {
  if (!slotStaffed(slot)) return `${slot.label}: open slot. Drag a role here to staff it.`
  return staffingSentence(slot, roleName)
}
