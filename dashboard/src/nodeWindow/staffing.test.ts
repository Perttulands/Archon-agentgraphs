import { describe, expect, it } from 'vitest'
import { formationTypeChoices } from '../components/FormationTypeChip'
import type { FormationNode, PersonaCard } from '../components/formationsTypes'
import { slotStaffed, staffingSentence } from './staffing'

const card = {
  id: 'critic', displayName: 'Critic', kind: 'judge', tags: [], harnessDefault: 'claude-code', etag: 'e',
  harnessVariants: [{ id: 'claude-code', sessionStem: 'critic', model: 'claude-opus-5', effort: 'xhigh' }],
} as unknown as PersonaCard

describe('staffingSentence', () => {
  it('reads a slot\'s own harness, model and effort, and a slot without a role as vanilla', () => {
    expect(staffingSentence({ id: 's', label: 'Worker', controller: false, harness: 'claude-code', model: 'opus', effort: 'low' }, undefined, undefined))
      .toBe('Worker is a vanilla agent on claude-code, model opus, low effort.')
    // The slot's settings win over the role card's.
    expect(staffingSentence({ id: 's', label: 'Judge', controller: true, agentId: 'critic', harness: 'openai-codex', effort: 'max' }, undefined, card))
      .toBe('Judge (controller) is Critic (critic) on openai-codex, default model, max effort.')
  })

  it('says a role slot without a harness has none, whatever its role card holds', () => {
    expect(staffingSentence({ id: 's', label: 'Judge', controller: false, agentId: 'critic' }, undefined, card))
      .toBe('Judge is Critic (critic) with no harness.')
  })

  it('counts a vanilla slot as staffed, and an empty one as not', () => {
    expect(slotStaffed({ id: 's', label: 'A', controller: false, harness: 'claude-code', effort: 'low' })).toBe(true)
    expect(slotStaffed({ id: 's', label: 'A', controller: false })).toBe(false)
    expect(staffingSentence({ id: 's', label: 'A', controller: false }, undefined, undefined)).toBe('A is not staffed.')
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
