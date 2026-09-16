import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RunPoint from './RunPoint'

const problems: Record<string, unknown[]> = {
  gate_adversarial: [
    { seq: 10, type: 'error', code: 'invalid_judge_result', reason: { text: 'missing verdict block', bytes: 21 } },
    { seq: 11, type: 'run_blocked', reason: { text: 'invalid judge result: missing or unterminated chrote-verdict block', bytes: 66 }, resumeAllowed: false },
  ],
  gate_framing: [
    { seq: 6, type: 'run_blocked', code: 'resume_after_verdict', reason: { text: 'human gate verdict recorded; resume required', bytes: 44 }, resumeAllowed: true },
  ],
}

describe('RunPoint', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const nodeId = String(input).match(/\/evidence\/nodes\/([^/]+)$/)?.[1] || ''
      const found = nodeId in problems
      return Promise.resolve({
        ok: found,
        status: found ? 200 : 404,
        headers: { get: () => '' },
        json: () => Promise.resolve(found
          ? { success: true, data: { evidence: { runId: 'run_1', nodeId, kind: 'gate', problems: problems[nodeId] } } }
          : { success: false, error: { code: 'NOT_FOUND', message: 'not found' } }),
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
    expect(fetch).toHaveBeenCalledWith('/api/formations/runs/run_1/evidence/nodes/gate_adversarial', expect.anything())
    expect(screen.getByTestId('run-point')).toHaveClass('blocked')
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
    rerender(<RunPoint runId="run_1" point={{ kind: 'blocked', nodeId: '', gate: false, blockSeq: 3 }} title="" onLocate={() => {}} />)
    expect(screen.queryByTestId('run-point')).toBeNull()
    rerender(<RunPoint runId="run_1" point={null} title="" onLocate={() => {}} />)
    expect(screen.queryByTestId('run-point')).toBeNull()
  })
})
