import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RunPoint from './RunPoint'

const text = (value: string) => ({ text: value, bytes: value.length })
const problems = [
  { seq: 6, type: 'run_blocked', code: 'resume_after_verdict', nodeIds: ['gate_framing'], reason: text('human gate verdict recorded; resume required'), resumeAllowed: true },
  { seq: 10, type: 'error', code: 'invalid_judge_result', nodeIds: ['gate_adversarial'], reason: text('missing verdict block') },
  { seq: 11, type: 'run_blocked', nodeIds: ['gate_adversarial'], reason: text('invalid judge result: missing or unterminated chrote-verdict block'), resumeAllowed: false },
  { seq: 14, type: 'run_blocked', nodeIds: ['fmn_map'], reason: text('coordinator restarted; completed-turn evidence required'), resumeAllowed: true },
  { seq: 17, type: 'error', code: 'wall_clock_exceeded', nodeIds: [], reason: text('wall clock limit exceeded') },
  { seq: 18, type: 'run_blocked', nodeIds: [], reason: text('wall clock limit exceeded'), resumeAllowed: true },
]

describe('RunPoint', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const found = String(input) === '/api/formations/runs/run_1/evidence/problems'
      return Promise.resolve({
        ok: found,
        status: found ? 200 : 404,
        headers: { get: () => '' },
        json: () => Promise.resolve(found ? { success: true, data: { problems } } : { success: false, error: { code: 'NOT_FOUND', message: 'not found' } }),
      } as unknown as Response)
    }))
  })
  afterEach(() => {
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

  it('names the running node with its attempt', () => {
    render(<RunPoint runId="run_1" point={{ kind: 'running', nodeId: 'fmn_draft', gate: false, attempt: 2 }} title="Draft the brief" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent('running Draft the brief (attempt 2)')
    expect(screen.getByTestId('run-point')).toHaveClass('running')
  })

  it('names the blocked node with the recorded reason', async () => {
    render(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'gate_adversarial', gate: true, blockSeq: 11 }} title="Adversarial review" onLocate={() => {}} />)
    expect(screen.getByTestId('run-point')).toHaveTextContent(/^blocked at Adversarial review$/)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent('blocked at Adversarial review: invalid judge result: missing or unterminated chrote-verdict block'))
    expect(fetch).toHaveBeenCalledWith('/api/formations/runs/run_1/evidence/problems', expect.anything())
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

  it('calls the block after an answer a pause', async () => {
    render(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: 'gate_framing', gate: true, blockSeq: 6 }} title="Framing review" onLocate={() => {}} />)
    await waitFor(() => expect(screen.getByTestId('run-point')).toHaveTextContent('paused at Framing review after your answer'))
    expect(screen.getByTestId('run-point')).toHaveClass('paused')
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
