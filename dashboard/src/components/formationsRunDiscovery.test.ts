import { describe, expect, it } from 'vitest'
import { chooseBoardRun, openRunsByAttention, readRunLink, runChoiceLabel, runLinkSearch } from './formationsRunDiscovery'
import type { RunStatusProjection } from './formationsTypes'

const run = (runId: string, status: string, final = false): RunStatusProjection => ({ runId, status, final, boardSlug: 'wayfinding', missionId: 'mis', eventCount: 1 })

describe('run discovery', () => {
  it('reads and writes the board and run link without touching other parameters', () => {
    expect(readRunLink('?board=wayfinding&run=run_01A')).toEqual({ board: 'wayfinding', run: 'run_01A' })
    expect(readRunLink('')).toEqual({ board: '', run: '' })
    expect(runLinkSearch('?theme=dark&run=run_old', { board: 'wayfinding', run: '' })).toBe('?theme=dark&board=wayfinding')
    expect(runLinkSearch('', { board: 'wayfinding', run: 'run_01A' })).toBe('?board=wayfinding&run=run_01A')
    expect(runLinkSearch('?board=x', { board: '', run: '' })).toBe('')
  })

  it('orders open runs by what needs the operator, newest first', () => {
    const runs = [run('run_01A', 'running'), run('run_01B', 'blocked'), run('run_01C', 'waiting_human'), run('run_01D', 'running'), run('run_01E', 'succeeded', true)]
    expect(openRunsByAttention(runs).map(item => item.runId)).toEqual(['run_01C', 'run_01D', 'run_01A', 'run_01B'])
  })

  it('keeps a pinned run, then prefers open runs, then the run already shown', () => {
    const runs = [run('run_01A', 'running'), run('run_01B', 'waiting_human')]
    expect(chooseBoardRun({ slug: 'wayfinding', runs, pinnedRunId: 'run_01Z', current: null })).toBe('run_01Z')
    expect(chooseBoardRun({ slug: 'wayfinding', runs, pinnedRunId: '', current: runs[0] })).toBe('run_01B')
    const finished = run('run_01F', 'succeeded', true)
    expect(chooseBoardRun({ slug: 'wayfinding', runs: [finished], pinnedRunId: '', current: finished })).toBe('run_01F')
    expect(chooseBoardRun({ slug: 'other', runs: [], pinnedRunId: '', current: finished })).toBe('')
    expect(runChoiceLabel({ ...runs[1], runId: 'run_01M2N5GB90F16SY7E54WG39BYE', beadId: 'form-3yd.10' })).toBe('waiting_human · …G39BYE · form-3yd.10')
  })
})
