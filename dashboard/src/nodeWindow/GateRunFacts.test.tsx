import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { NodeEvidence } from '../evidence/runEvidenceApi'
import { GateRunFacts } from './GateRunFacts'

const evidence = vi.hoisted(() => ({ current: null as NodeEvidence | null }))
vi.mock('../evidence/runEvidenceApi', () => ({ fetchNodeEvidence: () => Promise.resolve(evidence.current) }))

const text = (value: string) => ({ text: value, bytes: value.length })
const gate = (rest: Partial<NodeEvidence>): NodeEvidence => ({ runId: 'run_1', nodeId: 'gate', kind: 'gate', ...rest })

afterEach(() => { evidence.current = null })

describe('GateRunFacts', () => {
  it('says why the gate has no verdict and shows the verdict the judge returned', async () => {
    evidence.current = gate({
      evaluations: [{ seq: 4, kinds: ['formation'], criterion: text('ok'), kindResults: [],
        judgeFailures: [{ seq: 5, code: 'invalid', reason: text('judge verdict requires evidence'), returned: text('{"verdict":"fail"}') }] }],
      problems: [{ seq: 6, type: 'run_blocked', reason: text('invalid judge result: judge verdict requires evidence') }],
    })
    render(<GateRunFacts runId="run_1" gateId="gate" />)
    const facts = await screen.findByTestId('gate-run-gate')
    expect(facts).toHaveTextContent('invalid judge result: judge verdict requires evidence')
    expect(screen.getByLabelText('The judge returned')).toHaveTextContent('{"verdict":"fail"}')
  })

  it('offers asking the judge again or taking a fail only at a rejected verdict the run can resume from', async () => {
    const blocked = (resumeAllowed: boolean) => gate({
      evaluations: [{ seq: 4, kinds: ['formation'], criterion: text('ok'), kindResults: [], judgeFailures: [{ seq: 5, reason: text('no evidence') }] }],
      problems: [{ seq: 6, type: 'run_blocked', code: 'invalid_judge_result', resumeAllowed, reason: text('invalid judge result: no evidence') }],
    })
    const settled: boolean[] = []
    const onSettle = (takeFail: boolean) => { settled.push(takeFail); return Promise.resolve() }
    evidence.current = blocked(false)
    const first = render(<GateRunFacts runId="run_1" gateId="gate" onSettle={onSettle} />)
    await screen.findByTestId('gate-run-gate')
    expect(screen.queryByRole('button', { name: 'Take it as a fail' })).toBeNull()
    first.unmount()

    evidence.current = blocked(true)
    render(<GateRunFacts runId="run_1" gateId="gate" onSettle={onSettle} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Take it as a fail' }))
    expect(settled).toEqual([true])
  })

  it('states the latest verdict with its reason and round', async () => {
    evidence.current = gate({
      evaluations: [
        { seq: 2, attempt: 1, kinds: ['formation'], criterion: text('ok'), kindResults: [], verdict: { seq: 3, verdict: 'fail', reason: text('thumb too short'), evidence: [] } },
        { seq: 8, attempt: 2, kinds: ['formation'], criterion: text('ok'), kindResults: [], verdict: { seq: 9, verdict: 'pass', reason: text('matches the target'), evidence: [] } },
      ],
    })
    render(<GateRunFacts runId="run_1" gateId="gate" />)
    const facts = await screen.findByTestId('gate-run-gate')
    expect(facts).toHaveTextContent('pass · round 2')
    expect(facts).toHaveTextContent('matches the target')
    expect(facts).not.toHaveTextContent('thumb too short')
  })
})
