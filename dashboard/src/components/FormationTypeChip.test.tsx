import { describe, expect, it } from 'vitest'
import { formationTypeChoices } from './FormationTypeChip'
import type { FormationNode } from './formationsTypes'

const base: FormationNode = { id: 'fmn_x', type: 'peer', title: 'X', inputs: [], outputs: [], slots: [] }

describe('formation type choices', () => {
  it('never offers the current type or flow', () => {
    expect(formationTypeChoices(base).map(choice => choice.type)).toEqual(['solo', 'orchestrated'])
    expect(formationTypeChoices({ ...base, type: 'flow' }).map(choice => choice.label)).toEqual(['Solo', 'Peer', 'Orchestrated'])
  })

  it('asks which staffed slot to keep when solo would drop agents', () => {
    const staffed = { ...base, slots: [
      { id: 'slot_a', label: 'A', controller: false, agentId: 'mason' },
      { id: 'slot_b', label: '', controller: false },
      { id: 'slot_c', label: 'C', controller: false, agentId: 'hazel' },
    ] }
    expect(formationTypeChoices(staffed).filter(choice => choice.type === 'solo')).toEqual([
      { label: 'Solo, keeping A (mason)', type: 'solo', keepSlotId: 'slot_a' },
      { label: 'Solo, keeping C (hazel)', type: 'solo', keepSlotId: 'slot_c' },
    ])
  })
})
