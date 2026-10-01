import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RunPoint from './RunPoint'

const text = (value: string) => ({ text: value, bytes: value.length })
const problems = [
  { seq: 10, type: 'error', code: 'invalid_judge_result', nodeIds: ['gate_adversarial'], reason: text('missing verdict block') },
  { seq: 11, type: 'run_blocked', nodeIds: ['gate_adversarial'], reason: text('invalid judge result: missing or unterminated archon-verdict block'), resumeAllowed: false },
  { seq: 14, type: 'run_blocked', nodeIds: ['fmn_map'], reason: text('coordinator restarted; completed-turn evidence required'), resumeAllowed: true },
  { seq: 17, type: 'error', code: 'wall_clock_exceeded', nodeIds: [], reason: text('wall clock limit exceeded') },
  { seq: 18, type: 'run_blocked', nodeIds: [], reason: text('wall clock limit exceeded'), resumeAllowed: true },
  { seq: 22, type: 'run_blocked', code: 'resume_attempts_exhausted', nodeIds: ['fmn_draft'], reason: text('resume attempts exhausted'), resumeAllowed: false, limit: { kind: 'attempts', nodeId: 'fmn_draft', used: 3, max: 3 } },
  { seq: 25, type: 'run_blocked', code: 'max_dispatch_exceeded', nodeIds: ['fmn_critic'], reason: text('max dispatch exceeded'), resumeAllowed: false, limit: { kind: 'dispatches', nodeId: 'fmn_critic', used: 8, max: 8 } },
]
const ended = (end: object) => [{ seq: 51, type: 'run_blocked', nodeIds: ['fmn_exec'], reason: text('another user message interrupted the dispatched Claude turn'), resumeAllowed: true, resumedSeq: 52 }, end]
let served: unknown[] = problems

describe('RunPoint', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const found = String(input) === '/api/runs/run_1/evidence/problems'
      return Promise.resolve({
        ok: found,
        status: found ? 200 : 404,
        headers: { get: () => '' },
        json: () => Promise.resolve(found ? { success: true, data: { problems: served } } : { success: false, error: { code: 'NOT_FOUND', message: 'not found' } }),
      } as unknown as Response)
    }))
  })
  afterEach(() => {
    served = problems
    cleanup()
    vi.unstubAllGlobals()
  })

  it('names the gate waiting for the operator and locates it', () => {
    const onLocate = vi.fn()
    render(<RunPoint runId="run_1" point={{ kind: 'waiting', nodeId: 'gate_framing', gate: true }} title="Framing review" onLocate={onLocate} />)
    fireEvent.click(screen.getByRole('button', { name: 'waiting for you at Framing review' }))
    expect(onLocate).toHaveBeenCalledWith('gate_framing')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('says in its tooltip what a click does', () => {
    const { unmount } = render(<RunPoint runId="run_1" point={{ kind: 'waiting', nodeId: 'gate_framing', gate: true }} title="Framing review" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveAttribute('title', 'waiting for you at Framing review. Show it on the canvas.')
    unmount()
    render(<RunPoint runId="run_1" point={{ kind: 'waiting', nodeId: 'gate_framing', gate: true }} title="Framing review" onLocate={() => {}} action="Open the step" />)
    expect(screen.getByTestId('run-point')).toHaveAttribute('title', 'waiting for you at Framing review. Open the step.')
  })

  it('names the running node with its attempt', () => {
    render(<RunPoint runId="run_1" point={{ kind: 'running', nodeId: 'fmn_draft', gate: false, attempt: 2 }} title="Draft the brief" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent('running Draft the brief (attempt 2)')
    expect(screen.getByTestId('run-point')).toHaveClass('running')
  })

  it('names the blocked node with the recorded reason', async () => {
    render(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'gate_adversarial', gate: true, blockSeq: 11 }} title="Adversarial review" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent(/^blocked at Adversarial review$/)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent('blocked at Adversarial review: invalid judge result: missing or unterminated archon-verdict block'))
    expect(fetch).toHaveBeenCalledWith('/api/runs/run_1/evidence/problems', expect.anything())
    expect(screen.getByTestId('run-point')).toHaveClass('blocked')
  })

  it('gives the reason of a block that names no node', async () => {
    // A restart names its node only through the open dispatch; the run bar shows the node in flight.
    const { rerender } = render(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'fmn_map', gate: false, blockSeq: 14 }} title="Map the territory" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent('blocked at Map the territory: coordinator restarted; completed-turn evidence required'))

    // An exceeded wall clock names no node at all.
    rerender(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: '', gate: false, blockSeq: 18 }} title="" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent(/^blocked: wall clock limit exceeded$/))
    expect(screen.getByTestId('run-point')).toBeDisabled()
  })

  it('says the run is routing an answer the operator gave', () => {
    render(<RunPoint runId="run_1" point={{ kind: 'running', nodeId: 'gate_framing', gate: true, answered: true }} title="Framing review" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent(/^routing your answer at Framing review$/)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('names the limit a block exhausted', async () => {
    const { rerender } = render(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'fmn_draft', gate: false, blockSeq: 22 }} title="Draft" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent(/^blocked at Draft: Draft used 3 of 3 attempts$/))
    rerender(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'fmn_critic', gate: false, blockSeq: 25 }} title="Brief critic" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent(/^blocked at Brief critic: the run used 8 of 8 dispatches$/))
  })

  it('says why a failed run ended and who ended it, not an earlier resumed block', async () => {
    served = ended({ seq: 53, type: 'run_failed', code: 'coordinator_execution_failed', nodeIds: ['fmn_exec'], reason: text('completed recovery requires a single-slot formation'), actor: 'archond' })
    render(<RunPoint runId="run_1" point={{ kind: 'failed', nodeId: 'fmn_exec', gate: false }} title="Execution" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent(/^failed at Execution: completed recovery requires a single-slot formation · ended by Archon$/))
    expect(screen.getByTestId('run-point')).toHaveClass('failed')
  })

  it('says who canceled a run and why', async () => {
    served = [{ seq: 9, type: 'run_canceled', nodeIds: ['gate_review'], reason: text('the brief was wrong'), actor: 'agent:ui' }]
    render(<RunPoint runId="run_1" point={{ kind: 'canceled', nodeId: 'gate_review', gate: true }} title="Operator review" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent(/^canceled at Operator review$/)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent('canceled at Operator review by the operator in the cockpit: the brief was wrong'))
    expect(screen.getByTestId('run-point')).toHaveClass('canceled')
  })

  it('names the node a failed run stopped on and says nothing when none is known', () => {
    const { rerender } = render(<RunPoint runId="run_1" point={{ kind: 'failed', nodeId: 'fmn_exec', gate: false }} title="Execution" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent('failed at Execution')
    expect(screen.getByTestId('run-point')).toHaveClass('failed')
    rerender(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: '', gate: false }} title="" onLocate={() => {}} />)
    expect(screen.queryByTestId('run-point')).toBeNull()
    rerender(<RunPoint runId="run_1" point={null} title="" onLocate={() => {}} />)
    expect(screen.queryByTestId('run-point')).toBeNull()
  })
})
