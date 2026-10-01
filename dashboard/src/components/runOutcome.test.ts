import { describe, expect, it } from 'vitest'
import { gateRouteWords, problemHeadline, runActorLabel, runEndPhrase, runEndProblem, runLimitPhrase } from './runOutcome'
import type { GateRoute } from './formationsApi'

const text = (value: string) => ({ text: value, bytes: value.length })
const titles: Record<string, string> = { fmn_draft: 'Draft', fmn_publish: 'Publish', gate_review: 'Operator review' }
const titleOf = (nodeId: string) => titles[nodeId] || ''

describe('run outcome words', () => {
  it('names who ended a run in the operator\'s words', () => {
    expect(runActorLabel('agent:ui')).toBe('the operator in the cockpit')
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
    expect(runEndPhrase('canceled', 'Operator review', { reason: text('operator stop'), actor: 'agent:ui' }))
      .toBe('canceled at Operator review by the operator in the cockpit')
    expect(runEndPhrase('canceled', 'Draft', { reason: text('the brief was wrong'), actor: 'agent:ui' }))
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

  it('names the limit a block hit', () => {
    expect(runLimitPhrase({ kind: 'attempts', nodeId: 'fmn_draft', used: 3, max: 3 }, titleOf)).toBe('Draft used 3 of 3 attempts')
    expect(runLimitPhrase({ kind: 'dispatches', nodeId: 'fmn_draft', used: 8, max: 8 }, titleOf)).toBe('the run used 8 of 8 dispatches')
  })

  it('marks in evidence a block the run resumed past and the run end', () => {
    const names = { node: titleOf }
    expect(problemHeadline({ seq: 51, type: 'run_blocked', reason: text(''), resumeAllowed: true, resumedSeq: 52 }, names)).toBe('#51 · blocked · resumed at #52')
    expect(problemHeadline({ seq: 53, type: 'run_failed', code: 'coordinator_execution_failed', reason: text(''), actor: 'archond' }, names))
      .toBe('#53 · run failed · ended by Archon · coordinator_execution_failed')
    expect(problemHeadline({ seq: 9, type: 'run_canceled', reason: text(''), actor: 'agent:ui' }, names)).toBe('#9 · run canceled by the operator in the cockpit')
    expect(problemHeadline({ seq: 20, type: 'run_blocked', code: 'resume_attempts_exhausted', reason: text(''), resumeAllowed: false, limit: { kind: 'attempts', nodeId: 'fmn_draft', used: 3, max: 3 } }, names))
      .toBe('#20 · blocked · resume_attempts_exhausted · Draft used 3 of 3 attempts · not resumable')
  })
})

describe('gate route words', () => {
  const approve: GateRoute = { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1, maxAttempts: 3 }], dispatches: { kind: 'dispatches', used: 2, max: 20 } }
  const sendBack = (attempt: number, extra: Partial<GateRoute> = {}): GateRoute => ({
    verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt, maxAttempts: 3 }], dispatches: { kind: 'dispatches', used: 2, max: 20 }, ...extra,
  })

  it('names where Approve and Send back lead', () => {
    expect(gateRouteWords('pass', approve, titleOf)).toEqual({ button: 'Approve → Publish', outcome: 'Approve: Publish runs next.', blocks: false, last: false })
    expect(gateRouteWords('fail', sendBack(2), titleOf)).toEqual({
      button: 'Send back to Draft', outcome: 'Send back: Draft runs again with your response (attempt 2 of 3).', blocks: false, last: false,
    })
  })

  it('warns when a send-back takes the last attempt', () => {
    const words = gateRouteWords('fail', sendBack(3), titleOf)
    expect(words.last).toBe(true)
    expect(words.outcome).toBe('Send back: Draft runs again with your response (attempt 3 of 3, its last).')
  })

  it('says a send-back past the limit blocks the run for good', () => {
    const words = gateRouteWords('fail', sendBack(4, { limit: { kind: 'attempts', nodeId: 'fmn_draft', used: 3, max: 3 } }), titleOf)
    expect(words).toEqual({ button: 'Send back to Draft', outcome: 'Send back blocks the run: Draft used 3 of 3 attempts. It cannot resume.', blocks: true, last: false })
    const spent = gateRouteWords('pass', { ...approve, limit: { kind: 'dispatches', used: 20, max: 20 } }, titleOf)
    expect(spent.blocks).toBe(true)
    expect(spent.outcome).toBe('Approve blocks the run: the run used 20 of 20 dispatches. It cannot resume.')
  })

  it('says a send-back to a step that never ran runs it, not again', () => {
    expect(gateRouteWords('fail', { verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 1, maxAttempts: 3 }] }, titleOf).outcome)
      .toBe('Send back: Draft runs with your response.')
  })

  it('says a join receives the approval and waits for its other inputs', () => {
    expect(gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'fmn_publish', title: 'Publish', kind: 'formation', attempt: 1, maxAttempts: 3, waitsForInputs: true }] }, titleOf).outcome)
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

  it('counts the judge dispatch of a judge gate', () => {
    const words = gateRouteWords('pass', { verdict: 'pass', targets: [{ nodeId: 'gate_judged', title: 'Judged', kind: 'gate' }], dispatches: { kind: 'dispatches', used: 19, max: 20 }, dispatchesNeeded: 1 }, titleOf)
    expect(words).toEqual({ button: 'Approve → Judged', outcome: 'Approve: Judged receives it next; the run has 1 of 20 dispatches left.', blocks: false, last: true })
  })

  it('gives no attempt warning without an attempt limit', () => {
    const words = gateRouteWords('fail', { verdict: 'fail', targets: [{ nodeId: 'fmn_draft', title: 'Draft', kind: 'formation', attempt: 4 }] }, titleOf)
    expect(words).toEqual({ button: 'Send back to Draft', outcome: 'Send back: Draft runs again with your response.', blocks: false, last: false })
  })

  it('warns when a route takes the last dispatch', () => {
    const words = gateRouteWords('pass', { ...approve, dispatches: { kind: 'dispatches', used: 19, max: 20 } }, titleOf)
    expect(words.last).toBe(true)
    expect(words.outcome).toBe('Approve: Publish runs next; the run has 1 of 20 dispatches left.')
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
