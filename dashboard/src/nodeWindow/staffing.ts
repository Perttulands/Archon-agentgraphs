import type { AgentProjection, FormationSlot, PersonaCard } from '../components/formationsTypes'

/** Whether a slot names anything to run: a role, harness, model or effort. */
export function slotStaffed(slot: FormationSlot): boolean {
  return Boolean(slot.agentId || slot.harness || slot.model || slot.effort)
}

/**
 * "Worker 1 is Codex builder (codex) on openai-codex, model gpt-5, medium effort."
 * A slot owns its harness, model and effort; a slot without a role is a vanilla
 * agent. A slot written before slots owned their settings (a role, and no model
 * or effort of its own) still reads them from the role until it is migrated.
 */
export function staffingSentence(slot: FormationSlot, agent: AgentProjection | undefined, card: PersonaCard | undefined): string {
  const role = `${slot.label || slot.id}${slot.controller ? ' (controller)' : ''}`
  if (!slotStaffed(slot)) return `${role} is not staffed.`
  const name = card?.displayName || agent?.displayName || slot.agentId || ''
  const who = slot.agentId
    ? `${role} is ${name}${name === slot.agentId ? '' : ` (${slot.agentId})`}`
    : `${role} is a vanilla agent`
  const legacy = Boolean(slot.agentId) && !slot.model && !slot.effort
  const harness = slot.harness || (legacy ? card?.harnessDefault || agent?.harnessDefault || '' : '')
  const parts = [who, harness ? `on ${harness}` : 'with no harness']
  if (!harness) return `${parts.join(' ')}.`
  if (legacy) {
    const variant = card?.harnessVariants.find(item => item.id === harness)
    const model = variant?.model ? `model ${variant.model}` : 'default model'
    const effort = variant?.effort ? `${variant.effort} effort` : 'default effort'
    return `${parts.join(' ')}, ${model}, ${effort}.`
  }
  const model = slot.model ? `model ${slot.model}` : 'default model'
  const effort = slot.effort ? `${slot.effort} effort` : 'no effort'
  return `${parts.join(' ')}, ${model}, ${effort}.`
}
