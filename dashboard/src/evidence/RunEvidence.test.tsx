import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RunEvidence from './RunEvidence'
import { artifactRawUrl } from './runEvidenceApi'

function respond(data: unknown, status = 200) {
  return Promise.resolve({
    ok: status < 400,
    status,
    headers: { get: () => '' },
    json: () => Promise.resolve(status < 400 ? { success: true, data } : { success: false, error: { code: 'NOT_FOUND', message: 'run evidence not found' } }),
  } as unknown as Response)
}

const text = (value: string, extra: Partial<{ bytes: number; truncated: boolean }> = {}) => ({ text: value, bytes: value.length, ...extra })

const routes: Record<string, unknown> = {
  '/api/formations/runs/run_1/evidence/nodes/fmn_plan': {
    evidence: {
      runId: 'run_1', nodeId: 'fmn_plan', kind: 'formation',
      attempts: [{
        attempt: 1, startedSeq: 3, reason: 'initial',
        inputs: [{ fromNodeId: 'mis_a', fromPortId: 'out', text: text('Deliver the brief') }],
        dispatches: [{ seq: 5, slotId: 'plan', agentId: 'delivery-planner', harness: 'claude-code', brief: true, resultSeq: 9, status: 'ok' }],
        output: {
          seq: 10, status: 'done', text: text('Plan **written**', { bytes: 90000, truncated: true }),
          ports: [{ portId: 'port_plan_out', text: text('# Plan\n\nThree steps.'), ref: { artifact: 'plan.md' } }],
        },
      }],
    },
  },
  '/api/formations/runs/run_1/evidence/nodes/gate_review': {
    evidence: {
      runId: 'run_1', nodeId: 'gate_review', kind: 'gate',
      evaluations: [{
        seq: 20, attempt: 1, kinds: ['formation', 'human'], criterion: text('Beads pass lint'),
        input: { fromNodeId: 'fmn_beads', fromPortId: 'port_beads_out', text: text('beads text'), ref: { external: 'beads.md' } },
        kindResults: [{ seq: 22, kind: 'formation', verdict: 'pass', reason: text('All children link to the parent'), evidence: [{ kind: 'formation', text: text('bd lint: no warnings') }], evidenceOmitted: 3 }],
        humanRequests: [{ seq: 23, pending: false, decision: { seq: 24, verdict: 'pass', response: text('Ship it on Friday'), decidedBy: 'human:operator' } }],
        verdict: { seq: 25, verdict: 'pass', reason: text('Judge and operator agree'), perKind: { formation: 'pass', human: 'pass' }, routePort: 'pass', evidence: [] },
      }],
    },
  },
  '/api/formations/runs/run_1/evidence/artifacts': { artifacts: [{ name: 'plan.md', size: 4606, modifiedAt: '2026-09-09T01:21:00Z' }, { name: 'logs/run.json', size: 9, modifiedAt: '2026-09-09T01:22:00Z' }], truncated: false },
  '/api/formations/runs/run_1/evidence/artifacts/plan.md': { artifact: { name: 'plan.md', size: 4606, modifiedAt: '2026-09-09T01:21:00Z', kind: 'markdown', text: text('# Plan: stamp the build\n\nSee [the log](logs/run.json).') } },
  '/api/formations/runs/run_1/evidence/artifacts/logs/run.json': { artifact: { name: 'logs/run.json', size: 9, modifiedAt: '2026-09-09T01:22:00Z', kind: 'json', text: text('{"ok":true}') } },
  '/api/formations/runs/run_1/evidence/briefs/5': { brief: { dispatchSeq: 5, nodeId: 'fmn_plan', slotId: 'plan', attempt: 1, text: text('brief: plan the change\nartifact directory: …') } },
}

describe('RunEvidence', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      return url in routes ? respond(routes[url]) : respond(null, 404)
    }))
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders a node output, its ports and the truncation marker', async () => {
    render(<RunEvidence runId="run_1" nodeId="fmn_plan" title="Plan" state="done" onClose={() => {}} />)

    const output = await screen.findByTestId('node-output')
    expect(within(output).getByText('written').tagName).toBe('STRONG')
    expect(within(output).getByTestId('evidence-truncated')).toHaveTextContent('Showing 16 B of 88 KiB')
    const port = within(output).getByTestId('node-output-port-port_plan_out')
    expect(within(port).getByRole('heading', { name: 'Plan' })).toBeInTheDocument()
    expect(within(port).getByRole('link', { name: 'raw' })).toHaveAttribute('href', artifactRawUrl('run_1', 'plan.md'))
    expect(screen.getByTestId('node-attempt-1')).toHaveTextContent('delivery-planner · claude-code')

    fireEvent.click(within(screen.getByTestId('node-attempt-1')).getByRole('button', { name: 'brief' }))
    const brief = await screen.findByTestId('evidence-document')
    expect(brief).toHaveTextContent('Brief · plan attempt 1')
    expect(within(brief).getByLabelText('Brief · plan attempt 1', { selector: 'pre' })).toHaveTextContent('brief: plan the change')
  })

  it('renders a gate verdict reason with judge evidence and the operator response', async () => {
    render(<RunEvidence runId="run_1" nodeId="gate_review" title="Review" state="done" onClose={() => {}} />)

    const verdict = await screen.findByTestId('node-gate-verdict')
    expect(verdict).toHaveTextContent('verdict · pass · route pass')
    expect(verdict).toHaveTextContent('Judge and operator agree')
    const evaluation = screen.getByTestId('gate-evaluation-20')
    expect(evaluation).toHaveTextContent('All children link to the parent')
    expect(evaluation).toHaveTextContent('bd lint: no warnings')
    expect(evaluation).toHaveTextContent('3 more items not shown')
    expect(evaluation).toHaveTextContent('Ship it on Friday')
    expect(evaluation).toHaveTextContent('beads.md · outside the run')
  })

  it('opens an artifact from the run list and follows its links to other artifacts', async () => {
    render(<RunEvidence runId="run_1" nodeId="fmn_plan" title="Plan" state="done" onClose={() => {}} />)

    fireEvent.click(within(await screen.findByTestId('run-artifacts')).getByRole('button', { name: 'plan.md' }))
    const document = await screen.findByTestId('evidence-document')
    expect(within(document).getByRole('heading', { name: 'Plan: stamp the build' })).toBeInTheDocument()
    expect(within(document).getByRole('link', { name: 'open raw' })).toHaveAttribute('href', '/api/formations/runs/run_1/artifacts/plan.md')

    fireEvent.click(within(document).getByRole('link', { name: 'the log' }))
    await waitFor(() => expect(screen.getByTestId('evidence-document')).toHaveTextContent('logs/run.json'))
    expect(screen.getByLabelText('logs/run.json', { selector: 'pre' })).toHaveTextContent('"ok": true')
  })

  it('says when evidence is unavailable', async () => {
    render(<RunEvidence runId="run_1" nodeId="fmn_missing" title="Missing" state="" onClose={() => {}} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Evidence unavailable: run evidence not found')
  })
})
