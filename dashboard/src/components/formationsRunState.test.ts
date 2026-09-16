import { describe, expect, it } from 'vitest'
import {
  activeRunStorageKey,
  runEventReportRef,
  runEventResumeAllowed,
  runEventText,
  runStatusFromResponse,
  statusFromRunEvent,
  projectNodeStates,
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
