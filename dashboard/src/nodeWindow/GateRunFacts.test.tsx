import { render, screen } from '@testing-library/react'
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
