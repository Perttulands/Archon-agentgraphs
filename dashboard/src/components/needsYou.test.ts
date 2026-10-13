import { describe, expect, it } from 'vitest'
import { needsYouCount, pageTitle, runsOfLiveMissions } from './needsYou'
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

  it('counts them per mission, by identity', () => {
    expect(needsYouCount(runs, 'msn_scouting')).toBe(2)
  })

  it('counts them all in the page title, leaving out deleted missions', () => {
    expect(pageTitle(runsOfLiveMissions(runs, missions), 'Scouting')).toBe('(3) Scouting · Archon')
    expect(pageTitle([], '')).toBe('Archon')
  })
})
