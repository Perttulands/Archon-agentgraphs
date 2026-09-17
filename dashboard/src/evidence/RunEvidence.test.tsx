import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BoardDocument } from '../components/formationsTypes'
import RunEvidence from './RunEvidence'
import { FileWindowsLayer, FileWindowsProvider } from '../files/FileWindows'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'
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
  '/api/formations/runs/run_1/evidence/nodes/fmn_map': {
    evidence: {
      runId: 'run_1', nodeId: 'fmn_map', kind: 'formation',
      attempts: [{
        attempt: 1, startedSeq: 3, reason: 'initial',
        inputs: [{ edgeId: 'edge_start', fromNodeId: 'mis_way', fromPortId: 'out', toPortId: 'port_map_in', text: text('Weekly digest') }],
        dispatches: [{ seq: 5, slotId: 'slot_scout', agentId: 'codex-scout', harness: 'openai-codex', brief: true, status: 'ok' }],
        output: {
          seq: 7, status: 'done', text: text('The territory, mapped once'),
          ports: [{ portId: 'port_map_out', text: text('The territory, mapped once') }, { portId: 'port_map_copy', text: text('The territory, mapped once') }],
        },
      }],
    },
  },
  '/api/formations/runs/run_1/evidence/nodes/gate_framing': {
    evidence: {
      runId: 'run_1', nodeId: 'gate_framing', kind: 'gate',
      evaluations: [{
        seq: 8, kinds: ['human'], criterion: text('Pick a framing'),
        input: { edgeId: 'edge_frame', fromNodeId: 'fmn_map', fromPortId: 'port_map_out', text: text('The territory, mapped once') },
        kindResults: [], humanRequests: [{ seq: 9, pending: false, decision: { seq: 10, verdict: 'pass', response: text('Framing 2'), decidedBy: 'human:operator', relayedBy: 'slot_scout' } }],
        verdict: { seq: 11, verdict: 'pass', reason: text('Framing 2'), routePort: 'pass', evidence: [] },
      }],
      problems: [
        { seq: 12, type: 'run_blocked', code: 'resume_after_verdict', reason: text('human gate verdict recorded; resume required'), resumeAllowed: true },
        { seq: 30, type: 'run_blocked', reason: text('human gate verdict recorded; resume required'), resumeAllowed: true },
        { seq: 40, type: 'error', code: 'invalid_judge_result', reason: text('missing or unterminated chrote-verdict block') },
        { seq: 41, type: 'run_blocked', reason: text('invalid judge result: missing or unterminated chrote-verdict block'), resumeAllowed: false },
      ],
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
    expect(evaluation).toHaveTextContent('beads.md · file on the host')
    expect(evaluation).toHaveTextContent('judge · pass')
    expect(evaluation).toHaveTextContent('Criterion · judge · human')
    expect(evaluation.textContent).not.toMatch(/formation/)
  })

  it('names steps, ports and slots, keeps the IDs behind a disclosure and prints an output once', async () => {
    const board = {
      id: 'brd_way', slug: 'wayfinding', title: 'Wayfinding', rev: 1, etag: 'e', connections: [],
      missions: [{ id: 'mis_way', title: 'Wayfinding', goal: 'Digest', beadId: '' }],
      formations: [{ id: 'fmn_map', type: 'solo', title: 'Map the territory', slots: [{ id: 'slot_scout', label: 'Scout', controller: true }],
        inputs: [{ id: 'port_map_in', label: 'Brief' }], outputs: [{ id: 'port_map_out', label: 'Map' }, { id: 'port_map_copy', label: 'Copy' }] }],
      gates: [{ id: 'gate_framing', title: 'Framing review', kinds: ['human'], criterion: 'Pick a framing' }],
    } as BoardDocument
    render(<RunEvidence runId="run_1" nodeId="fmn_map" title="Map the territory" state="done" board={board} onClose={() => {}} />)

    const output = await screen.findByTestId('node-output')
    expect(within(output).getAllByText('The territory, mapped once')).toHaveLength(1)
    expect(within(output).queryByTestId('node-output-value')).toBeNull()
    expect(within(output).getByTestId('node-output-port-port_map_out')).toHaveTextContent(/^Map/)
    expect(within(output).getByTestId('node-output-port-port_map_copy')).toHaveTextContent('Same text as Map.')
    const attempt = screen.getByTestId('node-attempt-1')
    expect(attempt).toHaveTextContent('Input from Wayfinding')
    expect(attempt).toHaveTextContent('Scout')
    expect(within(attempt).getByRole('button', { name: 'brief' })).toBeInTheDocument()
    for (const id of ['fmn_map', 'port_map_out', 'slot_scout', 'mis_way', 'edge_start']) {
      expect(output.textContent + attempt.textContent).not.toContain(id)
    }
    const ids = screen.getByTestId('evidence-ids')
    expect(ids).not.toHaveAttribute('open')
    expect(within(ids).getByText('Map the territory').nextSibling).toHaveTextContent('fmn_map')
    expect(within(ids).getByText('Scout').nextSibling).toHaveTextContent('slot_scout')
    expect(within(ids).getByText('Map').nextSibling).toHaveTextContent('port_map_out')
    expect(within(ids).getByText('Connection from Wayfinding').nextSibling).toHaveTextContent('edge_start')

    cleanup()
    render(<RunEvidence runId="run_1" nodeId="gate_framing" title="Framing review" state="done" board={board} onClose={() => {}} />)
    expect(await screen.findByTestId('gate-evaluation-8')).toHaveTextContent('Input from Map the territory · Map')
    expect(screen.getByTestId('gate-evaluation-8')).not.toHaveTextContent('port_map_out')
    // A decision a seat recorded for the operator names that seat.
    expect(screen.getByTestId('gate-evaluation-8')).toHaveTextContent('pass · human:operator · via Scout in Map the territory')

    cleanup()
    const staffed = { ...board, formations: board.formations.map(formation => ({ ...formation, slots: [{ ...formation.slots[0], agentId: 'codex-scout' }] })) }
    render(<RunEvidence runId="run_1" nodeId="gate_framing" title="Framing review" state="done" board={staffed} onClose={() => {}} />)
    expect(await screen.findByTestId('gate-evaluation-8')).toHaveTextContent('pass · human:operator · via codex-scout')
    cleanup()
    render(<RunEvidence runId="run_1" nodeId="gate_framing" title="Framing review" state="done" onClose={() => {}} />)
    expect(await screen.findByTestId('gate-evaluation-8')).toHaveTextContent('pass · human:operator · via slot_scout')
  })

  it('lists a human pause apart from blocks and errors', async () => {
    render(<RunEvidence runId="run_1" nodeId="gate_framing" title="Framing review" state="blocked" onClose={() => {}} />)

    const pauses = await screen.findByTestId('evidence-pauses')
    expect(within(pauses).getByRole('heading', { name: 'Pauses' })).toBeInTheDocument()
    expect(pauses).toHaveTextContent("#12 · paused after the operator's answer")
    expect(pauses).toHaveTextContent("#30 · paused after the operator's answer")
    expect(pauses.querySelector('.fail')).toBeNull()
    const failures = screen.getByTestId('evidence-failures')
    expect(within(failures).getByRole('heading', { name: 'Blocks and errors' })).toBeInTheDocument()
    expect(failures.querySelectorAll('.node-evidence-verdict.fail')).toHaveLength(2)
    expect(failures).toHaveTextContent('#40 · error · invalid_judge_result')
    expect(failures).toHaveTextContent('#41 · blocked · not resumable')
    expect(failures).toHaveTextContent('invalid judge result: missing or unterminated chrote-verdict block')
    expect(failures).not.toHaveTextContent('resume required')
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

  it('closes an open document on Escape, then the evidence dialog', async () => {
    const onClose = vi.fn()
    render(<RunEvidence runId="run_1" nodeId="fmn_plan" title="Plan" state="done" onClose={onClose} />)
    fireEvent.click(within(await screen.findByTestId('run-artifacts')).getByRole('button', { name: 'plan.md' }))
    await screen.findByTestId('evidence-document')

    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByTestId('evidence-document')).toBeNull())
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('opens an artifact in a file window when the cockpit has them', async () => {
    function WithWindows() {
      const stack = useWindowManager()
      return (
        <FileWindowsProvider stack={stack}>
          <RunEvidence runId="run_1" nodeId="fmn_plan" title="Plan" state="done" onClose={() => {}} />
          <WindowManagerProvider stack={stack}><FileWindowsLayer /></WindowManagerProvider>
        </FileWindowsProvider>
      )
    }
    render(<WithWindows />)
    fireEvent.click(within(await screen.findByTestId('run-artifacts')).getByRole('button', { name: 'plan.md' }))
    const file = await screen.findByRole('dialog', { name: 'file plan.md' })
    expect(await within(file).findByRole('heading', { name: 'Plan: stamp the build' })).toBeInTheDocument()
    expect(file).toHaveTextContent('Plan · plan.md')
    expect(screen.queryByTestId('evidence-document')).toBeNull()
  })

  it('says when evidence is unavailable', async () => {
    render(<RunEvidence runId="run_1" nodeId="fmn_missing" title="Missing" state="" onClose={() => {}} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Evidence unavailable: run evidence not found')
  })
})
