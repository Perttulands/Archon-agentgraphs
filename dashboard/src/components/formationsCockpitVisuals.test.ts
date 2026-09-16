import { describe, expect, it } from 'vitest'
import { formationSummary, agentRole, agentState, groupRosterByHarness, initials } from './formationsCockpitVisuals'
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
  it('prefers the default harness, then unbound, then a generic agent label', () => {
    expect(agentRole(agent({ assignable: true, harnessDefault: 'openai-codex' }))).toBe('openai-codex')
    expect(agentRole(agent({ assignable: false, unbound: true }))).toBe('unbound')
    expect(agentRole(agent({ assignable: true }))).toBe('agent')
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

describe('groupRosterByHarness', () => {
  it('lists Codex, then Claude, then other harnesses, and omits empty groups', () => {
    const roster = [
      agent({ id: 'claude-one', assignable: true, harnessDefault: 'claude-code' }),
      agent({ id: 'hermes-one', assignable: true, harnessDefault: 'hermes' }),
      agent({ id: 'codex-one', assignable: true, harnessDefault: 'openai-codex' }),
      agent({ id: 'bare', assignable: true }),
    ]
    expect(groupRosterByHarness(roster).map(section => [section.id, section.agents.map(next => next.id)])).toEqual([
      ['codex', ['codex-one']],
      ['claude', ['claude-one']],
      ['other', ['hermes-one', 'bare']],
    ])
    expect(groupRosterByHarness(roster.slice(0, 1)).map(section => section.label)).toEqual(['Claude'])
  })
})
