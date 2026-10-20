import { describe, expect, it } from 'vitest'
import { runSessions, type ListedRun } from './MissionNavigator'

describe('runSessions', () => {
  it('uses the compact index sessions without requiring a ledger', () => {
    const sessions = [{ sessionName: 'two', nodeId: 'b', slotId: 's2' }, { sessionName: 'three', nodeId: 'c', slotId: 's3' }]
    const run = { runId: 'run_1', missionSlug: 'm', status: 'running', sessions } as ListedRun
    expect(runSessions(run)).toEqual(sessions)
  })
})
