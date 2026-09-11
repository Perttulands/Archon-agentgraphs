import { describe, expect, it } from 'vitest'
import { seatSocketUrl, type RunSeat } from './seatApi'

const seat: RunSeat = {
  runId: 'run_test', nodeId: 'fmn_test', nodeTitle: 'Test', slotId: 'slot_1', slotLabel: 'Controller',
  harness: 'claude-code', controller: true, createdSeq: 7, sessionName: 'test', state: 'live',
  columns: 123, rows: 41, terminalUrl: '/api/formations/runs/run_test/seats/7/terminal',
}
describe('owned seat URL', () => {
  it('requires the exact run, creation sequence and native grid', () => {
    expect(seatSocketUrl(seat)).toContain(seat.terminalUrl)
    for (const patch of [{ state: 'ended' as const }, { columns: 0 }, { rows: undefined },
      { createdSeq: 8 }, { terminalUrl: '/terminal/ws?arg=other' }, { terminalUrl: 'ws://other/seat' }]) {
      expect(seatSocketUrl({ ...seat, ...patch })).toBeNull()
    }
  })
})
