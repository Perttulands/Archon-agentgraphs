import { describe, expect, it } from 'vitest'
import { RECENT_FINISHED_RUNS, chooseBoardRun, openRunsByAttention, readRunLink, recentFinishedRuns, runChoiceLabel, runChoices, runLinkSearch } from './formationsRunDiscovery'
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

  it('offers recent finished runs after the open ones, and always the run shown', () => {
    const runs = [run('run_01A', 'succeeded', true), run('run_01B', 'running'), run('run_01C', 'failed', true), run('run_01D', 'waiting_human'), run('run_01E', 'canceled', true)]
    expect(recentFinishedRuns(runs).map(item => item.runId)).toEqual(['run_01E', 'run_01C', 'run_01A'])
    const choices = runChoices(runs, null)
    expect(choices.open.map(item => item.runId)).toEqual(['run_01D', 'run_01B'])
    expect(choices.finished.map(item => item.runId)).toEqual(['run_01E', 'run_01C', 'run_01A'])

    // The shown run keeps its own latest status: here it finished since the list was read.
    const shown = { ...runs[1], status: 'succeeded', final: true }
    const updated = runChoices(runs, shown)
    expect(updated.open.map(item => item.runId)).toEqual(['run_01D'])
    expect(updated.finished.map(item => `${item.runId}:${item.status}`)).toEqual(['run_01E:canceled', 'run_01C:failed', 'run_01B:succeeded', 'run_01A:succeeded'])

    const many = Array.from({ length: RECENT_FINISHED_RUNS + 3 }, (_, index) => run(`run_01${String(index).padStart(2, '0')}`, 'succeeded', true))
    expect(recentFinishedRuns(many)).toHaveLength(RECENT_FINISHED_RUNS)
    const oldest = many[0]
    const offered = runChoices(many, oldest).finished
    expect(offered[offered.length - 1]).toBe(oldest)
  })
})
