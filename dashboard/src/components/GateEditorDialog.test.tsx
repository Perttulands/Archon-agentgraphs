import { describe, expect, it } from 'vitest'
import { draftFromGate, gateFieldsFromDraft, gateFieldsFromGate, gateKindLabel } from './GateEditorDialog'

describe('gate editor fields', () => {
  it('round-trips a code gate and orders kinds as the editor shows them', () => {
    const draft = draftFromGate({ id: 'gate_x', title: 'Lint', kinds: ['code', 'human'], criterion: 'Clean', check: 'output_contains', checkVersion: '1', checkValue: 'LINT OK' })
    expect(draft).toEqual({ title: 'Lint', kinds: ['human', 'code'], criterion: 'Clean', profileKey: 'output_contains@1', checkValue: 'LINT OK' })
    expect(gateFieldsFromDraft(draft)).toEqual({ title: 'Lint', kinds: ['human', 'code'], criterion: 'Clean', check: 'output_contains', checkVersion: '1', checkValue: 'LINT OK' })
  })

  it('sends blank check fields when code is off or the profile is chosen later', () => {
    const base = { title: ' Review ', criterion: ' ', profileKey: 'output_absent@1', checkValue: 'x' }
    expect(gateFieldsFromDraft({ ...base, kinds: ['human'] })).toEqual({ title: 'Review', kinds: ['human'], criterion: '', check: '', checkVersion: '', checkValue: '' })
    expect(gateFieldsFromDraft({ ...base, kinds: ['code'], profileKey: '' })).toMatchObject({ check: '', checkVersion: '', checkValue: '' })
  })

  it('restores a gate without the code kind without its stale check', () => {
    expect(gateFieldsFromGate({ id: 'gate_x', title: '', kinds: ['formation'], criterion: '', check: 'output_absent', checkVersion: '1', checkValue: 'old' }))
      .toEqual({ title: '', kinds: ['formation'], criterion: '', check: '', checkVersion: '', checkValue: '' })
    expect(gateKindLabel('formation')).toBe('judge')
  })
})
