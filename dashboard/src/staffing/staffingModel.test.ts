import { describe, expect, it } from 'vitest'
import { catalog, roles } from '../test/staffingCatalog'
import {
  allEfforts, applyParsed, captionText, clampEffort, effortRefusal, effortsFor, freshStaffing, offCatalog, parseWords,
  retiredRolesOf, roleName, roleTrouble, rolesOf, staffingOf, suggestEffort, withEffort, withHarness, withModel, withRole,
  type Staffing,
} from './staffingModel'

const context = { label: 'Agent', step: 'New formation' }
const vanilla: Staffing = { role: '', harness: 'claude-code', model: 'opus', effort: 'low' }

describe('the slot model in the cockpit', () => {
  it('reads a slot, and an empty one as nothing', () => {
    expect(staffingOf({ id: 's', label: 'A', controller: false })).toBeNull()
    expect(staffingOf({ id: 's', label: 'A', controller: false, harness: 'claude-code', effort: 'low' })).toEqual({ role: '', harness: 'claude-code', model: '', effort: 'low' })
    expect(captionText({ role: '', harness: 'claude-code', model: '', effort: 'low' })).toBe('Claude Code · default model · low')
    expect(captionText(vanilla)).toBe('Claude Code · opus · low')
  })

  it('offers only assignable roles, never unbound sessions', () => {
    expect(rolesOf([
      { id: 'b', displayName: 'Beta', assignable: true },
      { id: 'a', displayName: 'Alpha', assignable: true, kind: 'reviewer', summary: 'Reviews.' },
      { id: 'retired', assignable: false },
      { id: 'session', assignable: false, unbound: true },
    ])).toEqual([{ id: 'a', name: 'Alpha', kind: 'reviewer', summary: 'Reviews.' }, { id: 'b', name: 'Beta', kind: '', summary: '' }])
  })

  it('starts a new slot as vanilla on the first harness and model, at the step\'s policy effort or low', () => {
    expect(freshStaffing(catalog, context)).toEqual(vanilla)
    // max is never suggested: a step named for a final review starts at xhigh.
    expect(freshStaffing(catalog, { label: 'Agent', step: 'Final review' })).toEqual({ ...vanilla, effort: 'xhigh' })
    expect(freshStaffing(catalog, { label: 'Agent', step: 'Release audit' })).toEqual(vanilla)
  })

  it('narrows the efforts to a known model\'s own (archon-n7u.50)', () => {
    expect(allEfforts(catalog)).toEqual(['low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    expect(effortsFor(catalog, 'openai-codex', 'gpt-5.5')).toEqual(['low', 'medium', 'high', 'xhigh'])
    expect(effortsFor(catalog, 'openai-codex', 'gpt-7-nova')).toContain('ultra')
    expect(effortsFor(catalog, 'openai-codex', '')).toContain('ultra')
    expect(clampEffort(catalog, 'openai-codex', 'gpt-5.5', 'ultra')).toBe('xhigh')
    expect(clampEffort(catalog, 'claude-code', 'opus', 'ultra')).toBe('max')
    expect(effortRefusal(catalog, 'claude-code', 'opus', 'ultra')).toBe('Claude Code goes up to max; ultra is Codex only')
    expect(effortRefusal(catalog, 'openai-codex', 'gpt-5.5', 'max')).toBe('gpt-5.5 goes up to xhigh')
    expect(withEffort(catalog, { ...vanilla, harness: 'openai-codex', model: 'gpt-5.5' }, 'max').refused).toBe('gpt-5.5 goes up to xhigh.')
  })

  it('warns of a model outside its harness\'s catalog, but not of the harness default', () => {
    expect(offCatalog(catalog, { harness: 'openai-codex', model: 'gpt-7-nova' })).toBe(true)
    expect(offCatalog(catalog, { harness: 'openai-codex', model: '' })).toBe(false)
    expect(offCatalog(catalog, { harness: 'claude-code', model: 'sonnet' })).toBe(false)
    expect(offCatalog({ ...catalog, harnesses: [{ ...catalog.harnesses[1], models: [] }] }, { harness: 'openai-codex', model: 'gpt-7-nova' })).toBe(false)
  })

  it('switches the harness with a model from the other harness, and says so', () => {
    const out = withModel(catalog, { ...vanilla, harness: 'openai-codex', model: 'gpt-6-astra', effort: 'ultra' }, 'claude-code', 'opus')
    expect(out.next).toEqual({ role: '', harness: 'claude-code', model: 'opus', effort: 'max' })
    expect(out.note).toBe('Harness is now Claude Code: opus runs there, not on Codex. Claude Code goes up to max, so ultra became max.')
    expect(withHarness(catalog, vanilla, 'openai-codex')).toEqual({ next: { ...vanilla, harness: 'openai-codex', model: 'gpt-6-astra' }, note: 'Model gpt-6-astra: the first one Codex lists.' })
  })
})

describe('a role landing on a slot', () => {
  it('gives a fresh slot the policy, and keeps a staffed slot\'s or a stated effort with the policy offered', () => {
    const critic = roles[0]
    const reason = 'reviewing falls under architecture and review'
    expect(withRole(catalog, context, { ...vanilla, effort: 'medium' }, critic, 'policy')).toEqual({
      next: { ...vanilla, role: 'critic-judge', effort: 'xhigh' }, note: `Effort xhigh: ${reason}.`,
    })
    expect(withRole(catalog, context, { ...vanilla, harness: 'openai-codex', model: 'gpt-5.5', effort: 'high' }, critic, 'slot')).toEqual({
      next: { role: 'critic-judge', harness: 'openai-codex', model: 'gpt-5.5', effort: 'high' },
      note: `Codex · gpt-5.5 · high stays: the slot keeps its settings. Policy suggests xhigh: ${reason}.`,
      offer: { effort: 'xhigh', reason },
    })
    // An effort chosen in the open sentence is stated: an empty slot keeps it too.
    expect(withRole(catalog, context, { ...vanilla, effort: 'high' }, critic, 'stated')).toEqual({
      next: { ...vanilla, role: 'critic-judge', effort: 'high' },
      note: `high stays: you chose it. Policy suggests xhigh: ${reason}.`,
      offer: { effort: 'xhigh', reason },
    })
    // A slot already on the policy's effort needs no offer.
    expect(withRole(catalog, context, { ...vanilla, effort: 'xhigh' }, critic, 'slot')).toEqual({
      next: { ...vanilla, role: 'critic-judge', effort: 'xhigh' }, note: `Effort xhigh: ${reason}.`,
    })
    expect(withRole(catalog, context, { ...vanilla, role: 'critic-judge', effort: 'xhigh' }, null, 'slot')).toEqual({
      next: { ...vanilla, effort: 'xhigh' }, note: 'Vanilla: no role. Claude Code · opus · xhigh stays.',
    })
  })

  it('reads the policy from the role\'s kind, its words only when the kind is not the policy\'s, then the step, citing the policy line', () => {
    const role = (kind: string, name = 'Helper', summary = '') => ({ id: 'r', name, kind, summary })
    for (const kind of ['verifier', 'scout']) expect(suggestEffort(catalog, role(kind), '')?.effort).toBe('low')
    for (const kind of ['builder', 'debugger', 'operator']) expect(suggestEffort(catalog, role(kind), '')?.effort).toBe('medium')
    for (const kind of ['reviewer', 'judge', 'architect', 'designer', 'planner', 'orchestrator']) expect(suggestEffort(catalog, role(kind), '')?.effort).toBe('xhigh')
    // The reason cites the policy line the effort comes from, never the kind back.
    expect(suggestEffort(catalog, role('planner'), '')).toEqual({ effort: 'xhigh', reason: 'planning falls under architecture and review' })
    expect(suggestEffort(catalog, role('builder'), '')).toEqual({ effort: 'medium', reason: 'building falls under making things' })
    // The kind decides over the name.
    expect(suggestEffort(catalog, role('verifier', 'Release Gatekeeper reviewer'), '')).toEqual({ effort: 'low', reason: 'verifying falls under errands' })
    // Without a kind the policy names, the name and summary are read; never max.
    expect(suggestEffort(catalog, roles[1], 'New formation')).toEqual({ effort: 'low', reason: 'Repo Scout reads as errands' })
    expect(suggestEffort(catalog, role('specialist', 'Final release auditor', 'Audits the release.'), '')).toBeNull()
    expect(suggestEffort(catalog, role('', 'Builder'), '')?.effort).toBe('medium')
    expect(suggestEffort(catalog, undefined, 'Final review')).toEqual({ effort: 'xhigh', reason: 'the step “Final review” reads as architecture and review' })
    expect(suggestEffort(catalog, undefined, 'New formation')).toBeNull()
  })
})

describe('a role a slot names', () => {
  it('is named and flagged when it is retired or gone', () => {
    const withRetired = { ...catalog, retired: [{ id: 'old', name: 'Old Critic', kind: 'reviewer', summary: '' }] }
    expect(roleTrouble(withRetired, 'critic-judge')).toBeNull()
    expect(roleTrouble(withRetired, '')).toBeNull()
    expect(roleTrouble(withRetired, 'old')).toBe('retired')
    expect(roleTrouble(withRetired, 'ghost')).toBe('missing')
    expect(roleName(withRetired, 'old')).toBe('Old Critic')
    expect(retiredRolesOf([{ id: 'old', displayName: 'Old Critic', kind: 'reviewer', assignable: false }, { id: 'live', assignable: true }, { id: 'tmux', assignable: false, unbound: true }]).map(role => role.id)).toEqual(['old'])
  })
})

describe('typed words', () => {
  it('reads a role and a model prefix, with the policy effort', () => {
    const parsed = parseWords(catalog, 'cri ast', vanilla)
    expect(parsed.issues).toEqual([])
    expect(parsed.read).toEqual([{ word: 'ast', as: 'model gpt-6-astra' }, { word: 'cri', as: 'role Critic Judge' }])
    expect(applyParsed(catalog, context, vanilla, parsed, 'policy').next).toEqual({ role: 'critic-judge', harness: 'openai-codex', model: 'gpt-6-astra', effort: 'xhigh' })
    // On a staffed slot the role keeps the slot's effort and offers the policy's.
    expect(applyParsed(catalog, context, vanilla, parsed, 'slot')).toMatchObject({ next: { role: 'critic-judge', effort: 'low' }, offer: { effort: 'xhigh' } })
  })

  it('keeps an effort typed with a role, even on an empty slot, and offers the policy, as a picked one is', () => {
    for (const words of ['critic high', 'high cri']) {
      expect(applyParsed(catalog, context, vanilla, parseWords(catalog, words, vanilla), 'policy')).toMatchObject({
        next: { role: 'critic-judge', effort: 'high' }, offer: { effort: 'xhigh' }, note: 'high stays: you chose it. Policy suggests xhigh: reviewing falls under architecture and review.',
      })
    }
  })

  it('names an effort the harness does not take and offers both fixes', () => {
    const parsed = parseWords(catalog, 'claude ultra', vanilla)
    expect(parsed.issues[0]).toEqual({ text: 'Claude Code goes up to max; ultra is Codex only.', blocking: true })
    expect(parsed.alternatives.map(alternative => alternative.label)).toEqual(['Claude Code · opus · max', 'Codex · gpt-6-astra · ultra'])
  })

  it('names a level a known Codex model lacks', () => {
    const parsed = parseWords(catalog, 'gpt-5.5 ultra', vanilla)
    expect(parsed.issues[0].text).toBe('gpt-5.5 goes up to xhigh.')
  })

  it('names a model from the other harness', () => {
    expect(parseWords(catalog, 'codex opus', vanilla).issues[0].text).toBe('opus runs on Claude Code, not Codex.')
  })

  it('offers the nearest model for a typo', () => {
    const parsed = parseWords(catalog, 'opsu', vanilla)
    expect(parsed.issues[0].text).toBe('No model “opsu”. Did you mean opus?')
    expect(parsed.alternatives[0].staffing.model).toBe('opus')
  })

  it('shows the choices for an ambiguous role word instead of committing one', () => {
    const parsed = parseWords({ ...catalog, roles: [...roles, { id: 'code-reviewer', name: 'Code Reviewer', summary: '', kind: 'reviewer' }, { id: 'final', name: 'Final Reviewer', summary: '', kind: 'reviewer' }] }, 'review', vanilla)
    expect(parsed.ambiguous?.roles.map(role => role.id)).toEqual(['code-reviewer', 'final'])
    expect(parsed.issues[0].blocking).toBe(true)
  })

  it('accepts a model outside the catalog with a warning', () => {
    const parsed = parseWords(catalog, 'gpt-7-nova', { ...vanilla, harness: 'openai-codex', model: 'gpt-6-astra' })
    expect(parsed.offCatalog).toBe('gpt-7-nova')
    expect(parsed.issues).toEqual([])
    expect(applyParsed(catalog, context, { ...vanilla, harness: 'openai-codex', model: 'gpt-6-astra' }, parsed, 'slot')).toMatchObject({
      next: { harness: 'openai-codex', model: 'gpt-7-nova' }, note: 'gpt-7-nova: not in the catalog; the harness decides.',
    })
  })

  it('drops the role on vanilla and keeps the rest', () => {
    const base = { ...vanilla, role: 'critic-judge', effort: 'xhigh' }
    expect(applyParsed(catalog, context, base, parseWords(catalog, 'vanilla', base), 'slot').next).toEqual({ ...vanilla, effort: 'xhigh' })
  })

  it('reads a harness word as that harness and its first model', () => {
    const parsed = parseWords(catalog, 'codex', vanilla)
    expect(applyParsed(catalog, context, vanilla, parsed, 'slot')).toEqual({
      next: { ...vanilla, harness: 'openai-codex', model: 'gpt-6-astra' }, note: 'Model gpt-6-astra: the first one Codex lists.', offer: undefined, effortTyped: false,
    })
  })
})
