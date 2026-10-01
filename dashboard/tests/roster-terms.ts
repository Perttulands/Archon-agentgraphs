/* What GET /api/agents serves beside the roster, as harness_launch.go,
 * harness_models.go and slot_settings.go build it: the harnesses Archon starts,
 * each with its efforts and known models (Codex's from a fixture
 * models_cache.json), and the effort policy. */

export const harnesses = [
  { id: 'claude-code', executable: 'claude', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium',
    models: [{ id: 'opus' }, { id: 'sonnet' }, { id: 'haiku' }, { id: 'fable' }] },
  { id: 'openai-codex', executable: 'codex', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], defaultEffort: 'medium',
    models: [
      { id: 'gpt-6-astra', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] },
      { id: 'gpt-5.6-sol', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] },
      { id: 'gpt-5.5', efforts: ['low', 'medium', 'high', 'xhigh'] },
    ] },
]

export const effortPolicy = [
  { effort: 'low', use: 'errands' },
  { effort: 'medium', use: 'making things' },
  { effort: 'xhigh', use: 'architecture and review' },
  { effort: 'max', use: 'consequential reviews' },
]

/** The roster answer: the agents with the harnesses and the effort policy. */
export function rosterAnswer<T>(agents: T[]) {
  return { agents, count: agents.length, harnesses, effortPolicy }
}

/** What the store's assignSlot writes, or why it refuses (formation_authoring.go assignmentSettings, slot_settings.go). */
export function assignedSettings(slotId: string, request: { agentId?: string; harness?: string; model?: string; effort?: string }): { settings: Record<string, string> } | { refused: string } {
  const role = (request.agentId || '').trim()
  const harnessId = (request.harness || '').trim()
  const model = (request.model || '').trim()
  const effort = (request.effort || '').trim()
  const name = `slot "${slotId}"`
  if (!role && !harnessId && !model && !effort) return { settings: {} }
  if (!harnessId) return { refused: `${name} needs a harness: claude-code or openai-codex` }
  const harness = harnesses.find(entry => entry.id === harnessId)
  if (!harness) return { refused: `${name} harness "${harnessId}" cannot start seats; use claude-code or openai-codex` }
  if (/\s/.test(model)) return { refused: `${name} model "${model}" must be one model name without spaces` }
  if (!effort) return { refused: `${name} needs an effort; the policy is low for errands, medium for making things, xhigh for architecture and review, max for consequential reviews` }
  const known = (harness.models as Array<{ id: string; efforts?: string[] }>).find(entry => entry.id === model && entry.efforts)
  const efforts = known?.efforts || harness.efforts
  if (!efforts.includes(effort)) return { refused: `${name} effort "${effort}" is not one ${known ? model : harness.id} accepts; use ${efforts.join(', ')}` }
  return { settings: { ...(role ? { agentId: role } : {}), harness: harnessId, ...(model ? { model } : {}), effort } }
}
