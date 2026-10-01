import { describe, expect, it } from 'vitest'
import { formationSummary, inputFeedLabel, agentRole, agentState, byRoleName, initials, outputRowStatus, rosterCountLabel } from './formationsCockpitVisuals'
import type { AgentProjection, FormationNode } from './formationsTypes'

const agent = (over: Partial<AgentProjection> & { assignable: boolean }): AgentProjection => ({ id: 'a', ...over })

describe('initials', () => {
  it('take the first two alphanumerics, uppercased', () => {
    expect(initials('lab-poet')).toBe('LA')
    expect(initials('Z')).toBe('Z')
    expect(initials('___')).toBe('?')
  })
})

describe('agentRole', () => {
  it('names a role by its kind, never a harness, and an unbound session as one', () => {
    expect(agentRole(agent({ assignable: true, harnessDefault: 'openai-codex', kind: 'reviewer' }))).toBe('reviewer')
    expect(agentRole(agent({ assignable: false, unbound: true }))).toBe('unbound')
    expect(agentRole(agent({ assignable: true, harnessDefault: 'claude-code' }))).toBe('role')
  })
})

describe('agentState', () => {
  it('is on when live or assignable, otherwise idle', () => {
    expect(agentState(agent({ assignable: false, liveness: 'live' }))).toBe('on')
    expect(agentState(agent({ assignable: true, liveness: 'dead' }))).toBe('on')
    expect(agentState(agent({ assignable: false, liveness: 'dead' }))).toBe('idle')
  })
})

describe('formationSummary', () => {
  const formation: FormationNode = { id: 'work', type: 'solo', title: 'Work', inputs: [], outputs: [], slots: [] }
  it('shows the authored brief as a single line', () => {
    expect(formationSummary({ ...formation, brief: { goal: '  Review the plan.\n Check evidence.  ' } }))
      .toBe('Review the plan. Check evidence.')
  })
  it('asks for a brief when none is supplied', () => {
    expect(formationSummary(formation)).toBe('Set a brief to describe this work.')
    expect(formationSummary({ ...formation, brief: { goal: '  ' } })).toBe('Set a brief to describe this work.')
  })
})

describe('inputFeedLabel', () => {
  const titles: Record<string, string> = { mis_start: 'Start', gate_review: 'Review', fmn_draft: 'Draft' }
  const titleOf = (nodeId: string) => titles[nodeId] || nodeId
  it('names every source of an input and marks fail and judge routes', () => {
    expect(inputFeedLabel([
      { id: 'a', from: 'mis_start:out', to: 'fmn_draft:in' },
      { id: 'b', from: 'gate_review:fail', to: 'fmn_draft:in' },
    ], titleOf)).toBe('from Start, Review (fail)')
    expect(inputFeedLabel([{ id: 'c', from: 'gate_review:judge', to: 'fmn_judge:in' }], titleOf)).toBe('from Review (judge)')
    expect(inputFeedLabel([{ id: 'd', from: 'gate_review:pass', to: 'fmn_next:in' }], titleOf)).toBe('from Review')
  })
  it('is empty for an unwired input', () => {
    expect(inputFeedLabel([], titleOf)).toBe('')
  })
})

describe('byRoleName', () => {
  it('lists roles by name whatever their default harness', () => {
    const roster = [
      agent({ id: 'claude-one', displayName: 'Zed', assignable: true, harnessDefault: 'claude-code' }),
      agent({ id: 'codex-one', displayName: 'Ada', assignable: true, harnessDefault: 'openai-codex' }),
      agent({ id: 'bare', assignable: true }),
    ]
    expect([...roster].sort(byRoleName).map(next => next.id)).toEqual(['codex-one', 'bare', 'claude-one'])
  })
})

describe('outputRowStatus', () => {
  it('reports what the selected run did with the node, and nothing without a run', () => {
    expect(outputRowStatus(false, 'done', true)).toEqual({ label: 'no output yet', tone: 'idle' })
    expect(outputRowStatus(true, 'done', true)).toEqual({ label: 'output ready', tone: 'done' })
    expect(outputRowStatus(true, undefined, true)).toEqual({ label: 'output ready', tone: 'done' })
    expect(outputRowStatus(true, 'running', true)).toEqual({ label: 'running', tone: 'running' })
    expect(outputRowStatus(true, 'waiting', false)).toEqual({ label: 'waiting', tone: 'review' })
    expect(outputRowStatus(true, 'blocked', true)).toEqual({ label: 'blocked', tone: 'blocked' })
    expect(outputRowStatus(true, 'failed', false)).toEqual({ label: 'failed', tone: 'blocked' })
    expect(outputRowStatus(true, undefined, false)).toEqual({ label: 'not reached', tone: 'idle' })
  })
})

describe('rosterCountLabel', () => {
  it('names the scope of the placed count so the two tabs never share one word for different facts', () => {
    expect(rosterCountLabel(25, { placed: 6, scope: 'canvas' })).toBe('25 · 6 on canvas')
    expect(rosterCountLabel(25, { live: 2, placed: 5, scope: 'mission' })).toBe('25 · 2 live · 5 on mission')
    expect(rosterCountLabel('…', { live: 0, placed: 0, scope: 'mission' })).toBe('…')
  })
})
