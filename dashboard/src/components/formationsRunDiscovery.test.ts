import { describe, expect, it } from 'vitest'
import { chooseBoardRun, openRunsByAttention, readRunLink, runLinkSearch } from './formationsRunDiscovery'
import type { RunStatusProjection } from './formationsTypes'

const run = (runId: string, status: string, final = false): RunStatusProjection => ({ runId, status, final, missionSlug: 'scouting', inputCardId: 'mis', eventCount: 1 })

describe('run discovery', () => {
  it('reads and writes the mission and run link without touching other parameters', () => {
    expect(readRunLink('?mission=scouting&run=run_01A')).toEqual({ board: 'scouting', run: 'run_01A' })
    expect(readRunLink('')).toEqual({ board: '', run: '' })
    expect(runLinkSearch('?theme=dark&run=run_old', { board: 'scouting', run: '' })).toBe('?mission=scouting&theme=dark')
    expect(runLinkSearch('', { board: 'scouting', run: 'run_01A' })).toBe('?mission=scouting&run=run_01A')
    expect(runLinkSearch('?mission=x', { board: '', run: '' })).toBe('')
  })

  it('keeps every other parameter after ?mission= and ?run=', () => {
    expect(readRunLink('?board=scouting&run=run_01A')).toEqual({ board: '', run: 'run_01A' })
    const link = '?theme=dark&mission=scouting&run=run_01A&view=flow'
    expect(runLinkSearch(link, readRunLink(link))).toBe('?mission=scouting&run=run_01A&theme=dark&view=flow')
  })

  it('orders open runs by what needs the operator, newest first', () => {
    const runs = [run('run_01A', 'running'), run('run_01B', 'blocked'), run('run_01C', 'waiting_human'), run('run_01D', 'running'), run('run_01E', 'succeeded', true)]
    expect(openRunsByAttention(runs).map(item => item.runId)).toEqual(['run_01C', 'run_01D', 'run_01A', 'run_01B'])
  })

  it('keeps a pinned run, then prefers open runs, then the run already shown', () => {
    const runs = [run('run_01A', 'running'), run('run_01B', 'waiting_human')]
    expect(chooseBoardRun({ slug: 'scouting', runs, pinnedRunId: 'run_01Z', current: null })).toBe('run_01Z')
    expect(chooseBoardRun({ slug: 'scouting', runs, pinnedRunId: '', current: runs[0] })).toBe('run_01B')
    const finished = run('run_01F', 'succeeded', true)
    expect(chooseBoardRun({ slug: 'scouting', runs: [finished], pinnedRunId: '', current: finished })).toBe('run_01F')
    expect(chooseBoardRun({ slug: 'other', runs: [], pinnedRunId: '', current: finished })).toBe('')
  })

})
