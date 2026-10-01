import { describe, expect, it } from 'vitest'
import { missionPickLabel, needsYouCount, otherRunsNeedingYou, pageTitle, runsOfLiveMissions } from './needsYou'
import type { BoardSummary, RunStatusProjection } from './formationsTypes'

const mission = (id: string, slug: string, title: string): BoardSummary => ({ id, slug, title, rev: 1, etag: '' })
const run = (runId: string, missionId: string, status: string, requestedAt = ''): RunStatusProjection => ({
  runId, missionId, missionSlug: missionId.replace('msn_', ''), status, final: false, inputCardId: 'inp', eventCount: 4,
  waitingGates: requestedAt ? [{ gateId: 'gate', requestedSeq: 4, requestedAt }] : [], updatedAt: requestedAt || '2026-10-01T10:00:00Z',
})

describe('runs that need you', () => {
  const missions = [mission('msn_scouting', 'scouting', 'Scouting'), mission('msn_delivery', 'delivery', 'Delivery')]
  const runs = [
    run('run_01A', 'msn_scouting', 'blocked'),
    run('run_01B', 'msn_scouting', 'waiting_human', '2026-10-01T12:00:00Z'),
    run('run_01C', 'msn_delivery', 'waiting_human', '2026-10-01T11:00:00Z'),
    run('run_01D', 'msn_deleted', 'waiting_human', '2026-10-01T09:00:00Z'),
  ]

  it('counts them per mission, by identity, on the mission picker', () => {
    expect(needsYouCount(runs, 'msn_scouting')).toBe(2)
    expect(missions.map(item => missionPickLabel(item, runs))).toEqual(['Scouting · 2 need you', 'Delivery · 1 needs you'])
    expect(missionPickLabel(mission('msn_quiet', 'quiet', ''), runs)).toBe('quiet')
  })

  it('counts them all in the page title, leaving out deleted missions', () => {
    expect(pageTitle(runsOfLiveMissions(runs, missions), 'Scouting')).toBe('(3) Scouting · Archon')
    expect(pageTitle([], '')).toBe('Archon')
  })

  it('offers the others, waiting before blocked and the longest waiting first, skipping deleted missions', () => {
    expect(otherRunsNeedingYou(runs, '', missions).map(item => item.runId)).toEqual(['run_01C', 'run_01B', 'run_01A'])
    expect(otherRunsNeedingYou(runs, 'run_01C', missions).map(item => item.runId)).toEqual(['run_01B', 'run_01A'])
  })
})
