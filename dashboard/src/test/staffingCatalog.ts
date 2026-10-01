import type { RoleEntry, StaffingCatalog } from '../staffing/staffingModel'

export const roles: RoleEntry[] = [
  { id: 'critic-judge', name: 'Critic Judge', summary: 'Reviews against acceptance.', kind: 'reviewer' },
  { id: 'repo-scout', name: 'Repo Scout', summary: 'Explores a codebase.', kind: 'specialist' },
  { id: 'wayfinding-opus-critic', name: 'Wayfinding Opus critic', summary: 'Independently reviews.', kind: 'reviewer' },
]

/** The daemon's catalog as GET /api/agents serves it, with the Codex models of a fixture cache. */
export const catalog: StaffingCatalog = {
  harnesses: [
    { id: 'claude-code', executable: 'claude', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium',
      models: [{ id: 'opus' }, { id: 'sonnet' }, { id: 'haiku' }, { id: 'fable' }] },
    { id: 'openai-codex', executable: 'codex', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], defaultEffort: 'medium',
      models: [
        { id: 'gpt-6-astra', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] },
        { id: 'gpt-5.6-sol', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] },
        { id: 'gpt-5.5', efforts: ['low', 'medium', 'high', 'xhigh'] },
      ] },
  ],
  roles,
  policy: [
    { effort: 'low', use: 'errands', kinds: ['verifier', 'scout', 'observer', 'operator'] },
    { effort: 'medium', use: 'making things', kinds: ['builder', 'debugger'] },
    { effort: 'xhigh', use: 'architecture and review', kinds: ['reviewer', 'judge', 'architect', 'planner', 'orchestrator'] },
    { effort: 'max', use: 'consequential reviews' },
  ],
}
