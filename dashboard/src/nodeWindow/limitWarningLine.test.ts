import { describe, expect, it } from 'vitest'
import { limitWarningLine } from './NodeWindow'

describe('a time card warning in a node\'s run', () => {
  it('names the attempt and the seat it was pasted into', () => {
    expect(limitWarningLine({ seq: 14, type: 'limit_warning', runId: 'run', nodeId: 'fmn_draft', attempt: 2, data: { slotId: 'worker' } }))
      .toBe('#14 · attempt 2 · time warning pasted into worker')
    expect(limitWarningLine({ seq: 14, type: 'limit_warning', runId: 'run', nodeId: 'fmn_draft', attempt: 2, data: { slotId: 'slot_1' } }, id => (id === 'slot_1' ? 'Worker' : '')))
      .toBe('#14 · attempt 2 · time warning pasted into Worker')
    // The lab records warnings without seats.
    expect(limitWarningLine({ seq: 9, type: 'limit_warning', runId: 'run', nodeId: 'fmn_draft', attempt: 1, data: {} }))
      .toBe('#9 · attempt 1 · time warning recorded')
  })
})
