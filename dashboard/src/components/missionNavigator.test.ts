import { describe, expect, it } from 'vitest'
import { runSessions, type ListedRun } from './MissionNavigator'

const run = (events: ListedRun['events']) => ({ runId: 'run_1', missionSlug: 'm', status: 'running', events }) as ListedRun

describe('runSessions', () => {
  it('lists the sessions a run still holds: an ended seat is gone, a seat kept on call stays', () => {
    expect(runSessions(run([
      { type: 'seat_created', nodeId: 'a', slotId: 's1', sessionName: 'one' },
      { type: 'seat_created', nodeId: 'b', slotId: 's2', sessionName: 'two' },
      { type: 'seat_created', nodeId: 'c', slotId: 's3', sessionName: 'three' },
      { type: 'seat_cleanup', nodeId: 'a', slotId: 's1', sessionName: 'one', outcome: 'ended' },
      { type: 'seat_cleanup', nodeId: 'b', slotId: 's2', sessionName: 'two', outcome: 'kept_on_call' },
    ])).map(session => session.sessionName)).toEqual(['two', 'three'])
  })
})
