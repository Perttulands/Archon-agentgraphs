import { describe, expect, it } from 'vitest'
import { gateRouteWords, problemHeadline, runActorLabel, runEndPhrase, runEndProblem, runLimitPhrase } from './runOutcome'
import type { GateRoute } from './formationsApi'

const text = (value: string) => ({ text: value, bytes: value.length })
const titles: Record<string, string> = { fmn_draft: 'Draft', fmn_publish: 'Publish', gate_review: 'Operator review' }
const titleOf = (nodeId: string) => titles[nodeId] || ''

describe('run outcome words', () => {
  it('names who ended a run in the operator\'s words', () => {
    expect(runActorLabel('human:ui')).toBe('the operator in the cockpit')
    expect(runActorLabel('agent:archon')).toBe('the archon CLI')
    expect(runActorLabel('archond')).toBe('Archon')
    // Inherited from the run's start, so it names the recorder, not who asked.
    expect(runActorLabel('operator:standalone')).toBe('the coordinator')
    expect(runActorLabel('human:perttu')).toBe('human:perttu')
    expect(runActorLabel()).toBe('')
  })

  it('says why a run failed or was canceled and who ended it', () => {
    expect(runEndPhrase('failed', 'Execution', { code: 'coordinator_execution_failed', reason: text('completed recovery requires a single-slot formation'), actor: 'archond' }))
      .toBe('failed at Execution: completed recovery requires a single-slot formation · ended by Archon')
    // The default reason adds nothing to who stopped it.
    expect(runEndPhrase('canceled', 'Operator review', { reason: text('operator stop'), actor: 'human:ui' }))
      .toBe('canceled at Operator review by the operator in the cockpit')
    expect(runEndPhrase('canceled', 'Draft', { reason: text('the brief was wrong'), actor: 'human:ui' }))
      .toBe('canceled at Draft by the operator in the cockpit: the brief was wrong')
    // A failure with only a code still says something.
    expect(runEndPhrase('failed', '', { code: 'coordinator_execution_failed', reason: text('') })).toBe('failed: coordinator_execution_failed')
    expect(runEndPhrase('canceled', '', undefined)).toBe('canceled')
  })

  it('finds the run end among a run\'s problems', () => {
    const problems = [
      { seq: 51, type: 'run_blocked', reason: text('interrupted'), resumedSeq: 52 },
      { seq: 53, type: 'run_failed', reason: text('completed recovery requires a single-slot formation'), actor: 'archond' },
    ]
    expect(runEndProblem(problems)?.seq).toBe(53)
    expect(runEndProblem(problems.slice(0, 1))).toBeUndefined()
  })

  it('names the Limit card a block found spent', () => {
    expect(runLimitPhrase({ kind: 'rounds', limitId: 'lim_cap', nodeId: 'fmn_draft', used: 3, max: 3 }, titleOf)).toBe('Draft used 3 of 3 rounds')
    // The card on the Input card covers the whole mission.
    expect(runLimitPhrase({ kind: 'rounds', limitId: 'lim_all', nodeId: 'mis', used: 20, max: 20, granted: 1 }, titleOf, id => id === 'mis'))
      .toBe('the mission used 20 of 20 rounds, 1 of them granted')
    expect(runLimitPhrase({ kind: 'rounds', limitId: 'lim_one', nodeId: 'fmn_draft', used: 1, max: 1 }, titleOf)).toBe('Draft used 1 of 1 round')
  })

  it('marks in evidence a block the run resumed past and the run end', () => {
    const names = { node: titleOf }
    expect(problemHeadline({ seq: 51, type: 'run_blocked', reason: text(''), resumeAllowed: true, resumedSeq: 52 }, names)).toBe('#51 · blocked · resumed at #52')
    expect(problemHeadline({ seq: 53, type: 'run_failed', code: 'coordinator_execution_failed', reason: text(''), actor: 'archond' }, names))
      .toBe('#53 · run failed · ended by Archon · coordinator_execution_failed')
    expect(problemHeadline({ seq: 9, type: 'run_canceled', reason: text(''), actor: 'human:ui' }, names)).toBe('#9 · run canceled by the operator in the cockpit')
    expect(problemHeadline({ seq: 20, type: 'run_blocked', code: 'limit_reached', reason: text('Draft used 3 of 3 rounds'), resumeAllowed: true, limit: { kind: 'rounds', limitId: 'lim_cap', nodeId: 'fmn_draft', used: 3, max: 3 } }, names))
      .toBe('#20 · blocked · limit_reached · Draft used 3 of 3 rounds · resumable')
  })
})

describe('gate route words', () => {
  const cap = (used: number, max = 3) => ({ kind: 'rounds' as const, limitId: 'lim_cap', nodeId: 'fmn_draft', used, max })
  const approve: GateRoute = { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1 }] }
  const sendBack = (attempt: number, extra: Partial<GateRoute> = {}): GateRoute => ({
    verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt, rounds: cap(attempt - 1) }], ...extra,
  })

  it('names where Approve and Send back lead, with the round a Limit card allows', () => {
    expect(gateRouteWords('pass', approve, titleOf)).toEqual({ button: 'Approve → Publish', outcome: 'Approve: Publish runs next.', blocks: false, last: false })
    expect(gateRouteWords('fail', sendBack(2), titleOf)).toEqual({
      button: 'Send back to Draft', outcome: 'Send back: Draft runs again with your response (round 2 of 3).', blocks: false, last: false,
    })
  })

  it('warns when a send-back takes the last round', () => {
    const words = gateRouteWords('fail', sendBack(3), titleOf)
    expect(words.last).toBe(true)
    expect(words.outcome).toBe('Send back: Draft runs again with your response (round 3 of 3).')
  })

  it('says a send-back to a spent card blocks the run until a grant', () => {
    const words = gateRouteWords('fail', sendBack(4, { limit: cap(3) }), titleOf)
    expect(words).toEqual({
      button: 'Send back to Draft',
      outcome: 'Send back: Draft runs again with your response, but Draft has used all 3 of its rounds, so the run blocks instead until you grant one more round.',
      blocks: true,
      last: false,
    })
    const one = gateRouteWords('fail', { verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 2, rounds: cap(1, 1) }], limit: cap(1, 1) }, titleOf)
    expect(one.outcome).toBe('Send back: Draft runs again with your response, but Draft has used its only round, so the run blocks instead until you grant one more round.')
  })

  it('says when the mission card is spent', () => {
    const mission = { kind: 'rounds' as const, limitId: 'lim_all', nodeId: 'mis', used: 20, max: 20 }
    const spent = gateRouteWords('pass', { ...approve, missionRounds: mission, roundsNeeded: 1, limit: mission }, titleOf)
    expect(spent.blocks).toBe(true)
    expect(spent.outcome).toBe('Approve: Publish runs next, but the mission has used all 20 of its rounds, so the run blocks instead until you grant one more round.')
  })

  it('names the time a step and the mission have left, and a spent time card\'s grant', () => {
    const clock = (used: number, max = 1800) => ({ kind: 'time' as const, limitId: 'lim_clock', nodeId: 'fmn_draft', used, max })
    const missionTime = { kind: 'time' as const, limitId: 'lim_all', nodeId: 'mis', used: 600, max: 3600 }
    const back = (extra: Partial<GateRoute> = {}): GateRoute => ({ verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 2, rounds: cap(1), time: clock(300) }], ...extra })
    expect(gateRouteWords('fail', back(), titleOf).outcome).toBe('Send back: Draft runs again with your response (round 2 of 3, 25 min of 30 min left).')
    expect(gateRouteWords('fail', back({ missionTime }), titleOf).outcome)
      .toBe('Send back: Draft runs again with your response (round 2 of 3, 25 min of 30 min left); the mission has 50 min of 1 h of working time left.')
    const spent = gateRouteWords('fail', back({ targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 2, time: clock(1800) }], limit: clock(1800) }), titleOf)
    expect(spent.blocks).toBe(true)
    expect(spent.outcome).toBe('Send back: Draft runs again with your response, but Draft has used all 30 min of its time, so the run blocks instead until you grant 30 min more.')
    const missionSpent = gateRouteWords('pass', { ...approve, missionTime: { ...missionTime, used: 3600 }, limit: { ...missionTime, used: 3600 } }, titleOf)
    expect(missionSpent.outcome).toBe('Approve: Publish runs next, but the mission has used all 1 h of its time, so the run blocks instead until you grant 1 h more.')
    // A route that only ends its path says nothing of the mission's time.
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'end_done', title: 'Done', kind: 'end', outcome: 'done' }], missionTime }, titleOf).outcome)
      .toBe('Approve: this path ends (done); the run goes on with its other work.')
  })

  it('words a spent time card in a block headline', () => {
    expect(runLimitPhrase({ kind: 'time', limitId: 'lim_clock', nodeId: 'fmn_draft', used: 1800, max: 1800 }, titleOf)).toBe('Draft used 30 min of 30 min')
    expect(runLimitPhrase({ kind: 'time', limitId: 'lim_all', nodeId: 'mis', used: 60, max: 60, granted: 30 }, titleOf, id => id === 'mis')).toBe('the mission used 1 min of 1 min, 30 s of it granted')
  })

  it('says a send-back to a step that never ran runs it, not again', () => {
    expect(gateRouteWords('fail', { verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 1 }] }, titleOf).outcome)
      .toBe('Send back: Draft runs with your response.')
  })

  it('says a join receives the approval and waits for its other inputs', () => {
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1, waitsForInputs: true }] }, titleOf).outcome)
      .toBe('Approve: Publish receives this and waits for its other inputs.')
  })

  const done = { nodeId: 'end_done', title: 'Done', kind: 'end', outcome: 'done' as const }
  const rejected = { nodeId: 'end_rejected', title: 'Rejected', kind: 'end', outcome: 'rejected' as const }

  it('says a path ends at an End node while the run goes on with its other work', () => {
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [done] }, titleOf)).toEqual({ button: 'Approve', outcome: 'Approve: this path ends (done); the run goes on with its other work.', blocks: false, last: false })
    // A rejected End fails the run once the rest of its open work has ended.
    expect(gateRouteWords('fail', { verdict: 'fail', targets: [rejected], runFails: true }, titleOf).outcome).toBe('Send back: this path ends (rejected), so the run fails once its other open work ends.')
    // After a path was already rejected, a route on to a step says so too.
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1 }], runFails: true }, titleOf).outcome)
      .toBe('Approve: Publish runs next; the run fails once its other open work ends.')
  })

  it('names a step and an End node on one route', () => {
    const words = gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1 }, done] }, titleOf)
    expect(words).toEqual({ button: 'Approve → Publish', outcome: 'Approve: Publish runs next; this path ends (done).', blocks: false, last: false })
  })

  it('counts the judge run of a judge gate against the mission card', () => {
    const missionRounds = { kind: 'rounds' as const, limitId: 'lim_all', nodeId: 'mis', used: 19, max: 20 }
    const words = gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'gate_judged', title: 'Judged', kind: 'gate' }], missionRounds, roundsNeeded: 1 }, titleOf)
    expect(words).toEqual({ button: 'Approve → Judged', outcome: 'Approve: Judged receives it next; the mission has 1 of 20 rounds left.', blocks: false, last: true })
    // With rounds to spare, the mission card goes unmentioned.
    expect(gateRouteWords('pass', { ...approve, missionRounds: { ...missionRounds, used: 2 }, roundsNeeded: 1 }, titleOf).outcome).toBe('Approve: Publish runs next.')
  })

  it('gives no round without a Limit card', () => {
    const words = gateRouteWords('fail', { verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 4 }] }, titleOf)
    expect(words).toEqual({ button: 'Send back to Draft', outcome: 'Send back: Draft runs again with your response.', blocks: false, last: false })
  })

  it('says when a verdict ends the run, and whether the run then succeeds or fails', () => {
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [done], endsRun: true }, titleOf)).toEqual({
      button: 'Approve and end the run', outcome: 'Approve: this path ends (done), and with nothing else to run, the run succeeds.', blocks: false, last: false,
    })
    expect(gateRouteWords('fail', { verdict: 'fail', targets: [rejected], endsRun: true, runFails: true }, titleOf)).toEqual({
      button: 'Send back and fail the run', outcome: 'Send back: this path ends (rejected), and with nothing else to run, the run fails.', blocks: false, last: false,
    })
    // A path already rejected elsewhere fails the run even when this one ends done.
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [done], endsRun: true, runFails: true }, titleOf).button).toBe('Approve and fail the run')
  })

  it('keeps the plain verb for a route with no targets', () => {
    expect(gateRouteWords('fail', { verdict: 'fail', targets: [] }, titleOf)).toEqual({ button: 'Send back', outcome: '', blocks: false, last: false })
  })

  it('keeps the plain verbs without routes', () => {
    expect(gateRouteWords('pass', undefined, titleOf)).toEqual({ button: 'Approve', outcome: '', blocks: false, last: false })
    expect(gateRouteWords('fail', undefined, titleOf).button).toBe('Send back')
  })
})
