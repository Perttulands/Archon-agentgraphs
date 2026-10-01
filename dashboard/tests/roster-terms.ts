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
