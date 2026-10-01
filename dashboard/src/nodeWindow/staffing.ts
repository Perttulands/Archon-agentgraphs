import type { AgentProjection, FormationSlot, PersonaCard } from '../components/formationsTypes'

/** Whether a slot names anything to run: a role, harness, model or effort. */
export function slotStaffed(slot: FormationSlot): boolean {
  return Boolean(slot.agentId || slot.harness || slot.model || slot.effort)
}

/**
 * "Worker 1 is Codex builder (codex) on openai-codex, model gpt-5, medium effort."
 * A slot owns its harness, model and effort; a slot without a role is a vanilla
 * agent.
 */
export function staffingSentence(slot: FormationSlot, agent: AgentProjection | undefined, card: PersonaCard | undefined): string {
  const role = `${slot.label || slot.id}${slot.controller ? ' (controller)' : ''}`
  if (!slotStaffed(slot)) return `${role} is not staffed.`
  const name = card?.displayName || agent?.displayName || slot.agentId || ''
  const who = slot.agentId
    ? `${role} is ${name}${name === slot.agentId ? '' : ` (${slot.agentId})`}`
    : `${role} is a vanilla agent`
  const harness = slot.harness || ''
  const parts = [who, harness ? `on ${harness}` : 'with no harness']
  if (!harness) return `${parts.join(' ')}.`
  const model = slot.model ? `model ${slot.model}` : 'default model'
  const effort = slot.effort ? `${slot.effort} effort` : 'no effort'
  return `${parts.join(' ')}, ${model}, ${effort}.`
}
