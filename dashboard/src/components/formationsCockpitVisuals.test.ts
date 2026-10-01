import { describe, expect, it } from 'vitest'
import { formationSummary, inputFeedLabel, agentRole, agentState, byRoleName, initials, inSlotsWords, outputRowStatus, roleUses, rolesInUseLabel } from './formationsCockpitVisuals'
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
    expect(agentRole(agent({ assignable: true, kind: 'reviewer' }))).toBe('reviewer')
    expect(agentRole(agent({ assignable: false, unbound: true }))).toBe('unbound')
    expect(agentRole(agent({ assignable: true }))).toBe('role')
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
      agent({ id: 'claude-one', displayName: 'Zed', assignable: true }),
      agent({ id: 'codex-one', displayName: 'Ada', assignable: true }),
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

describe('rolesInUseLabel', () => {
  it('labels every count in the same words on both rosters', () => {
    expect(rolesInUseLabel(25, 6)).toBe('25 roles · 6 in use')
    expect(rolesInUseLabel(25, 5, 2)).toBe('25 roles · 5 in use · 2 live')
    expect(rolesInUseLabel(1, 0)).toBe('1 role · 0 in use')
    expect(rolesInUseLabel('…', 0)).toBe('… roles · 0 in use')
  })

  it('counts each role\'s slots across the mission and says so in words', () => {
    const formation = (id: string, roles: Array<string | undefined>) => ({ id, type: 'peer', title: id, inputs: [], outputs: [], slots: roles.map((agentId, index) => ({ id: `${id}-${index}`, label: 'A', controller: false, ...(agentId ? { agentId } : {}) })) }) as FormationNode
    const uses = roleUses([formation('a', ['critic', 'builder']), formation('b', ['critic', undefined])])
    expect([...uses]).toEqual([['critic', 2], ['builder', 1]])
    expect(inSlotsWords(2)).toBe('in 2 slots')
    expect(inSlotsWords(1)).toBe('in 1 slot')
    expect(inSlotsWords(0)).toBe('')
  })
})
