import type { AgentProjection, FormationSlot, PersonaCard } from '../components/formationsTypes'

/** "Worker 1 is Codex builder (codex) on openai-codex, model gpt-5, medium effort." A persona that sets neither uses its harness's defaults, and says so. */
export function staffingSentence(slot: FormationSlot, agent: AgentProjection | undefined, card: PersonaCard | undefined): string {
  const role = `${slot.label || slot.id}${slot.controller ? ' (controller)' : ''}`
  if (!slot.agentId) return `${role} is not staffed.`
  const name = card?.displayName || agent?.displayName || slot.agentId
  const harness = slot.harness || card?.harnessDefault || agent?.harnessDefault || ''
  const variant = card?.harnessVariants.find(item => item.id === harness)
  const parts = [
    `${role} is ${name}${name === slot.agentId ? '' : ` (${slot.agentId})`}`,
    harness ? `on ${harness}` : 'with no harness',
  ]
  if (!harness) return `${parts.join(' ')}.`
  const model = variant?.model ? `model ${variant.model}` : 'default model'
  const effort = variant?.effort ? `${variant.effort} effort` : 'default effort'
  return `${parts.join(' ')}, ${model}, ${effort}.`
}
