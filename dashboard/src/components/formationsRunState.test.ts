import { describe, expect, it } from 'vitest'
import {
  activeRunStorageKey,
  runEventReportRef,
  runEventResumeAllowed,
  runEventText,
  runStatusFromResponse,
  statusFromRunEvent,
  projectNodeStates,
  runCurrentPoint,
  runPointPhrase,
  upsertRunEvent,
} from './formationsRunState'
import type { RunEvent, RunStatusProjection } from './formationsTypes'

describe('formations run-state helpers', () => {
  it('deduplicates run events by run and sequence while keeping timeline order', () => {
    const first: RunEvent = { runId: 'run_1', seq: 2, type: 'node_started', nodeId: 'fmn_work' }
    const second: RunEvent = { runId: 'run_1', seq: 1, type: 'run_started' }
    const replacement: RunEvent = { runId: 'run_1', seq: 2, type: 'node_output', nodeId: 'fmn_work' }

    const events = upsertRunEvent(upsertRunEvent(upsertRunEvent([], first), second), replacement)

    expect(events.map(event => `${event.seq}:${event.type}`)).toEqual([
      '1:run_started',
      '2:node_output',
    ])
    expect(upsertRunEvent(events, { runId: '', seq: 3, type: 'node_started' })).toBe(events)
  })

  it('maps ledger events to status labels without turning in-flight blocks into final success', () => {
    expect(statusFromRunEvent({ runId: 'run_1', seq: 1, type: 'run_started' })).toBe('running')
    expect(statusFromRunEvent({ runId: 'run_1', seq: 2, type: 'run_blocked' })).toBe('blocked')
    expect(statusFromRunEvent({ runId: 'run_1', seq: 3, type: 'run_succeeded' })).toBe('succeeded')
    expect(statusFromRunEvent({ runId: 'run_1', seq: 4, type: 'node_output' })).toBe('')
  })

  it('keeps historical inline-verification verdicts non-authorizing in node projection', () => {
    const states = projectNodeStates([
      { runId: 'run_1', seq: 1, type: 'node_output', nodeId: 'fmn_work', data: { status: 'done' } },
      { runId: 'run_1', seq: 2, type: 'verification_verdict', nodeId: 'fmn_work', data: { verdict: 'fail' } },
    ], null)

    expect(states.get('fmn_work')).toBe('done')
  })

  it('returns a blocked node to its prior state once the run resumes', () => {
    const answered: RunEvent[] = [
      { runId: 'run_1', seq: 1, type: 'human_input_requested', gateId: 'gate_review', nodeId: 'gate_review' },
      { runId: 'run_1', seq: 2, type: 'human_verdict_recorded', gateId: 'gate_review', nodeId: 'gate_review' },
      { runId: 'run_1', seq: 3, type: 'gate_verdict', gateId: 'gate_review', nodeId: 'gate_review', data: { verdict: 'pass' } },
      { runId: 'run_1', seq: 4, type: 'run_blocked', gateId: 'gate_review', data: { reason: 'human gate verdict recorded; resume required' } },
    ]
    expect(projectNodeStates(answered, null).get('gate_review')).toBe('blocked')

    const resumed = projectNodeStates([
      ...answered,
      { runId: 'run_1', seq: 5, type: 'run_resumed', data: { resumeMode: 'reattach' } },
      { runId: 'run_1', seq: 6, type: 'node_started', nodeId: 'fmn_next' },
      { runId: 'run_1', seq: 7, type: 'node_started', nodeId: 'fmn_lost' },
      { runId: 'run_1', seq: 8, type: 'run_blocked', nodeId: 'fmn_lost', data: { reason: 'seat lost' } },
    ], null)
    expect(resumed.get('gate_review')).toBe('done')
    expect(resumed.get('fmn_next')).toBe('running')
    expect(resumed.get('fmn_lost')).toBe('blocked')

    const judged = projectNodeStates([
      { runId: 'run_1', seq: 1, type: 'run_blocked', gateId: 'gate_judge', nodeId: 'gate_judge' },
      { runId: 'run_1', seq: 2, type: 'run_resumed' },
    ], null)
    expect(judged.get('gate_judge')).toBe('')
  })

  const run = (status: string, extra: Partial<RunStatusProjection> = {}): RunStatusProjection => ({
    runId: 'run_1', status, final: status === 'failed', boardSlug: 'wayfinding', missionId: 'mis_a', eventCount: 9, ...extra,
  })

  it('names the gate a run waits at for the operator', () => {
    const events: RunEvent[] = [
      { runId: 'run_1', seq: 1, type: 'node_started', nodeId: 'fmn_map', attempt: 1 },
      { runId: 'run_1', seq: 2, type: 'node_output', nodeId: 'fmn_map', data: { status: 'done' } },
      { runId: 'run_1', seq: 3, type: 'gate_evaluating', nodeId: 'gate_framing', gateId: 'gate_framing' },
      { runId: 'run_1', seq: 4, type: 'human_input_requested', nodeId: 'gate_framing', gateId: 'gate_framing' },
    ]
    const point = runCurrentPoint(events, run('waiting_human', { waitingGates: [{ gateId: 'gate_framing', requestedSeq: 4 }] }))
    expect(point).toEqual({ kind: 'waiting', nodeId: 'gate_framing', gate: true })
    expect(runPointPhrase(point!, 'Framing review')).toBe('waiting for you at Framing review')
    expect(projectNodeStates(events).get('gate_framing')).toBe('waiting')
  })

  it('names the running node with its attempt', () => {
    const events: RunEvent[] = [
      { runId: 'run_1', seq: 1, type: 'node_started', nodeId: 'fmn_draft', attempt: 1 },
      { runId: 'run_1', seq: 2, type: 'node_output', nodeId: 'fmn_draft', data: { status: 'done' } },
      { runId: 'run_1', seq: 3, type: 'gate_evaluating', nodeId: 'gate_critic', gateId: 'gate_critic' },
      { runId: 'run_1', seq: 4, type: 'gate_verdict', nodeId: 'gate_critic', gateId: 'gate_critic', data: { verdict: 'fail' } },
      { runId: 'run_1', seq: 5, type: 'node_started', nodeId: 'fmn_draft', attempt: 2 },
      { runId: 'run_1', seq: 6, type: 'slot_dispatch', nodeId: 'fmn_draft', attempt: 2, data: { slotId: 'slot_writer' } },
    ]
    const point = runCurrentPoint(events, run('running'))
    expect(point).toEqual({ kind: 'running', nodeId: 'fmn_draft', gate: false, attempt: 2 })
    expect(runPointPhrase(point!, 'Draft the brief')).toBe('running Draft the brief (attempt 2)')
    expect(runPointPhrase({ kind: 'running', nodeId: 'fmn_draft', gate: false, attempt: 1 }, 'Draft the brief')).toBe('running Draft the brief')
    expect(runPointPhrase({ kind: 'running', nodeId: 'gate_critic', gate: true }, 'Adversarial review')).toBe('evaluating Adversarial review')
    expect(runCurrentPoint(events.slice(0, 2), run('running'))).toBeNull()
  })

  it('names the blocked node and the block whose reason explains it', () => {
    const events: RunEvent[] = [
      { runId: 'run_1', seq: 7, type: 'gate_evaluating', nodeId: 'gate_adversarial', gateId: 'gate_adversarial' },
      { runId: 'run_1', seq: 8, type: 'node_started', nodeId: 'fmn_critic', attempt: 1 },
      { runId: 'run_1', seq: 9, type: 'node_output', nodeId: 'fmn_critic', data: { status: 'done' } },
      { runId: 'run_1', seq: 10, type: 'judge_attempt_failed', nodeId: 'gate_adversarial', gateId: 'gate_adversarial' },
      { runId: 'run_1', seq: 11, type: 'run_blocked', nodeId: 'gate_adversarial', gateId: 'gate_adversarial' },
    ]
    const point = runCurrentPoint(events, run('blocked', { resumeAllowed: false }))
    expect(point).toEqual({ kind: 'blocked', nodeId: 'gate_adversarial', gate: true, blockSeq: 11 })
    expect(projectNodeStates(events).get('gate_adversarial')).toBe('blocked')
    expect(runPointPhrase(point!, 'Adversarial review', 'invalid judge result')).toBe('blocked at Adversarial review: invalid judge result')
    expect(runPointPhrase(point!, 'Adversarial review')).toBe('blocked at Adversarial review')
    expect(runPointPhrase(point!, 'Framing review', 'human gate verdict recorded; resume required', true)).toBe('paused at Framing review after your answer')

    // A restart block names no node: the node in flight is where the run stopped.
    const restarted: RunEvent[] = [
      { runId: 'run_1', seq: 1, type: 'node_started', nodeId: 'fmn_map', attempt: 1 },
      { runId: 'run_1', seq: 2, type: 'error' },
      { runId: 'run_1', seq: 3, type: 'run_blocked' },
    ]
    expect(runCurrentPoint(restarted, run('blocked'))).toEqual({ kind: 'blocked', nodeId: 'fmn_map', gate: false, blockSeq: 3 })
    expect(projectNodeStates(restarted).get('fmn_map')).toBe('blocked')
    expect(projectNodeStates([...restarted, { runId: 'run_1', seq: 4, type: 'run_resumed' }]).get('fmn_map')).toBe('running')
    expect(runPointPhrase({ kind: 'blocked', nodeId: '', gate: false, blockSeq: 3 }, '')).toBe('blocked')
  })

  it('names the node a failed run stopped on', () => {
    const events: RunEvent[] = [
      { runId: 'run_1', seq: 1, type: 'node_started', nodeId: 'fmn_exec', attempt: 3 },
      { runId: 'run_1', seq: 2, type: 'gate_verdict', nodeId: 'gate_old', gateId: 'gate_old', data: { verdict: 'fail' } },
      { runId: 'run_1', seq: 3, type: 'run_failed' },
    ]
    const point = runCurrentPoint(events, run('failed'))
    expect(point).toEqual({ kind: 'failed', nodeId: 'fmn_exec', gate: false })
    expect(projectNodeStates(events).get('fmn_exec')).toBe('failed')
    expect(runPointPhrase(point!, 'Execution')).toBe('failed at Execution')
    expect(runPointPhrase({ kind: 'failed', nodeId: '', gate: false }, '')).toBe('')
    expect(runCurrentPoint(events, run('succeeded'))).toBeNull()
    expect(runCurrentPoint(events, null)).toBeNull()
  })

  it('extracts run text, report references, and resume affordance from events', () => {
    expect(runEventText({ runId: 'run_1', seq: 1, type: 'run_blocked', data: { reason: 'needs human' } })).toBe('needs human')
    expect(runEventText({ runId: 'run_1', seq: 2, type: 'node_output', data: { text: 'report body' } })).toBe('report body')
    expect(runEventReportRef({ runId: 'run_1', seq: 2, type: 'node_output', data: { reportRef: 'reports/fmn_work.md' } })).toBe('reports/fmn_work.md')
    expect(runEventResumeAllowed({ runId: 'run_1', seq: 3, type: 'run_blocked', data: { resumeAllowed: true } }, false)).toBe(true)
    expect(runEventResumeAllowed({ runId: 'run_1', seq: 4, type: 'run_failed' }, true)).toBe(false)
  })

  it('normalizes run status envelopes and active run storage keys', () => {
    const flat: RunStatusProjection = {
      runId: 'run_flat',
      status: 'running',
      final: false,
      boardSlug: 'session-search',
      missionId: 'mis_showcase',
      eventCount: 1,
    }
    const nested = { status: { ...flat, runId: 'run_nested', eventCount: 2 } }

    expect(runStatusFromResponse(flat).runId).toBe('run_flat')
    expect(runStatusFromResponse(nested).runId).toBe('run_nested')
    expect(activeRunStorageKey('session-search')).toBe('chrote-formations-active-run-session-search')
  })
})
