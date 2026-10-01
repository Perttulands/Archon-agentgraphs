import { describe, expect, it } from 'vitest'
import { inputLabel, missingInputs, missionRunInputs, suppliedInputs } from './missionInputs'
import type { MissionInput } from './formationsTypes'

describe('mission inputs', () => {
  it('takes the implicit required brief, described by the input hint, when none are declared', () => {
    expect(missionRunInputs(undefined)).toEqual([{ name: 'brief', kind: 'text', required: true, description: '' }])
    expect(missionRunInputs({ id: 'inp', title: 'Start', goal: '', inputHint: 'A raw sketch', inputs: [] })).toEqual([{ name: 'brief', kind: 'text', required: true, description: 'A raw sketch' }])
    const declared: MissionInput[] = [{ name: 'topic', kind: 'text' }]
    expect(missionRunInputs({ id: 'inp', title: 'Start', goal: '', inputHint: 'ignored', inputs: declared })).toBe(declared)
  })

  it('labels a name, finds the blank required ones and sends what was given', () => {
    expect(inputLabel('sketch_path')).toBe('Sketch path')
    expect(inputLabel('brief')).toBe('Brief')
    const inputs: MissionInput[] = [{ name: 'topic', kind: 'text', required: true }, { name: 'repo', kind: 'folder', required: true }, { name: 'notes', kind: 'file' }]
    expect(missingInputs(inputs, { topic: ' \n', notes: '/n' })).toEqual(['topic', 'repo'])
    expect(suppliedInputs(inputs, { topic: '  keep spacing ', repo: ' /work ', notes: '  ', stray: 'x' })).toEqual({ topic: '  keep spacing ', repo: '/work' })
  })
})
