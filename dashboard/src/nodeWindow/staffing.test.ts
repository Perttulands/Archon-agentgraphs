import { describe, expect, it } from 'vitest'
import { formationTypeChoices } from '../components/FormationTypeChip'
import type { FormationNode } from '../components/formationsTypes'
import { slotStaffed, staffingSentence } from './staffing'

const roleName = (id: string) => (id === 'critic' ? 'Critic' : id)

describe('staffingSentence', () => {
  it('reads a slot\'s own harness, model and effort, and a slot without a role as vanilla', () => {
    expect(staffingSentence({ id: 's', label: 'Worker', controller: false, harness: 'claude-code', model: 'opus', effort: 'low' }, roleName))
      .toBe('Worker is vanilla on Claude Code · opus · low.')
    expect(staffingSentence({ id: 's', label: 'Judge', controller: true, agentId: 'critic', harness: 'openai-codex', effort: 'max' }, roleName))
      .toBe('Judge (controller) is Critic on Codex · default model · max.')
  })

  it('says a role slot without a harness or effort has none', () => {
    expect(staffingSentence({ id: 's', label: 'Judge', controller: false, agentId: 'critic' }, roleName))
      .toBe('Judge is Critic on no harness · default model · no effort.')
  })

  it('counts a vanilla slot as staffed, and an empty one as not', () => {
    expect(slotStaffed({ id: 's', label: 'A', controller: false, harness: 'claude-code', effort: 'low' })).toBe(true)
    expect(slotStaffed({ id: 's', label: 'A', controller: false })).toBe(false)
    expect(staffingSentence({ id: 's', label: 'A', controller: false }, roleName)).toBe('A is not staffed.')
  })
})

describe('formationTypeChoices', () => {
  it('offers to keep a vanilla slot when changing to solo', () => {
    const formation = {
      id: 'f', type: 'peer', title: 'F', inputs: [], outputs: [],
      slots: [
        { id: 'a', label: 'A', controller: false, harness: 'claude-code', effort: 'low' },
        { id: 'b', label: 'B', controller: false, agentId: 'critic', harness: 'claude-code', effort: 'xhigh' },
      ],
    } as unknown as FormationNode
    expect(formationTypeChoices(formation).map(choice => choice.label)).toEqual(['Solo, keeping A (vanilla)', 'Solo, keeping B (critic)', 'Orchestrated'])
  })
})
