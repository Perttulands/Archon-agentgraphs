import { describe, expect, it } from 'vitest'
import type { BoardDocument } from '../components/formationsTypes'
import type { NodeEvidence } from '../evidence/runEvidenceApi'
import { latestOutputSeqs, producedFromEvidence, summarizeProduced } from './produced'

const text = (value: string) => ({ text: value, bytes: value.length })

function evidence(nodeId: string, seq: number, output: { text: string; ports: Array<{ portId: string; text: string; artifact?: string }> }): NodeEvidence {
  return {
    runId: 'run_1', nodeId, kind: 'formation',
    attempts: [
      { attempt: 1, inputs: [], dispatches: [], output: { seq: seq - 5, text: text('older'), ports: [] } },
      {
        attempt: 2, inputs: [], dispatches: [],
        output: { seq, text: text(output.text), ports: output.ports.map(port => ({ portId: port.portId, text: text(port.text), ref: port.artifact ? { artifact: port.artifact } : undefined })) },
      },
    ],
  }
}

describe('what a step produced', () => {
  it('lists the artifacts its ports name, port text without a file, and a report that says something new', () => {
    const plan = producedFromEvidence(evidence('fmn_plan', 11, {
      text: 'Wrote the plan.',
      ports: [{ portId: 'port_plan', text: '# Plan', artifact: 'plan.md' }, { portId: 'port_notes', text: 'Risks: none' }, { portId: 'port_copy', text: '# Plan', artifact: 'plan.md' }],
    }))
    expect(plan?.seq).toBe(11)
    expect(plan?.items).toEqual([
      { key: 'fmn_plan:artifact:plan.md', nodeId: 'fmn_plan', kind: 'artifact', artifact: 'plan.md', portId: 'port_plan', bytes: 6 },
      { key: 'fmn_plan:port:port_notes', nodeId: 'fmn_plan', kind: 'output', portId: 'port_notes', bytes: 11 },
      { key: 'fmn_plan:report', nodeId: 'fmn_plan', kind: 'output', bytes: 15 },
    ])

    const echoed = producedFromEvidence(evidence('fmn_lab', 4, { text: 'same text', ports: [{ portId: 'port_out', text: 'same text' }] }))
    expect(echoed?.items.map(item => item.key)).toEqual(['fmn_lab:port:port_out'])
    expect(producedFromEvidence({ runId: 'run_1', nodeId: 'fmn_idle', kind: 'formation', attempts: [] })).toBeNull()
  })

  it('reads the latest output sequence of each node from the events', () => {
    expect([...latestOutputSeqs([
      { runId: 'run_1', seq: 3, type: 'node_output', nodeId: 'fmn_a' },
      { runId: 'run_1', seq: 5, type: 'node_started', nodeId: 'fmn_b' },
      { runId: 'run_1', seq: 9, type: 'node_output', nodeId: 'fmn_a' },
    ])]).toEqual([['fmn_a', 9]])
  })
})

describe('what a run produced', () => {
  const board = {
    id: 'brd', slug: 'delivery', title: 'Delivery', rev: 1, etag: 'e',
    formations: ['fmn_plan', 'fmn_exec', 'fmn_judge', 'fmn_final'].map(id => ({ id, type: 'solo', title: id, inputs: [], outputs: [{ id: 'out', label: 'Result' }], slots: [] })),
    gates: [{ id: 'gate_review', title: 'Review', kinds: ['formation'], criterion: '' }],
    connections: [
      { id: 'c1', from: 'fmn_plan:out', to: 'fmn_exec:in' },
      { id: 'c2', from: 'fmn_exec:out', to: 'gate_review:in' },
      { id: 'c3', from: 'gate_review:judge', to: 'fmn_judge:in' },
      { id: 'c4', from: 'fmn_judge:out', to: 'gate_review:judge' },
      { id: 'c5', from: 'gate_review:pass', to: 'fmn_final:in' },
    ],
  } as BoardDocument
  const step = (nodeId: string, seq: number, artifact: string) => ({
    nodeId, seq, items: [
      { key: `${nodeId}:report`, nodeId, kind: 'output' as const, bytes: 10 },
      { key: `${nodeId}:artifact:${artifact}`, nodeId, kind: 'artifact' as const, artifact, portId: 'out', bytes: 20 },
    ],
  })
  const produced = [step('fmn_plan', 11, 'plan.md'), step('fmn_exec', 48, 'execution.md'), step('fmn_judge', 52, 'verdict.md'), step('fmn_final', 56, 'final-review.md')]
  const artifacts = ['plan.md', 'execution.md', 'final-review.md', 'worker1-health.txt'].map(name => ({ name, size: 100, modifiedAt: '' }))

  it('leads a finished run with its final step, artifact first, and lists the rest with unclaimed files', () => {
    const summary = summarizeProduced(board, produced, artifacts, true)
    expect(summary.primary.map(item => item.key)).toEqual(['fmn_final:artifact:final-review.md', 'fmn_final:report'])
    expect(summary.others.map(item => item.key)).toEqual([
      'fmn_judge:artifact:verdict.md', 'fmn_judge:report',
      'fmn_exec:artifact:execution.md', 'fmn_exec:report',
      'fmn_plan:artifact:plan.md', 'fmn_plan:report',
      'artifact:worker1-health.txt',
    ])
  })

  it('leads a running run with its latest work step, not the judge answering a gate', () => {
    const running = produced.slice(0, 3)
    expect(summarizeProduced(board, running, artifacts, false).primary.map(item => item.key)).toEqual(['fmn_exec:artifact:execution.md', 'fmn_exec:report'])
    // A finished run whose last step feeds a gate is led by its latest work step.
    expect(summarizeProduced(board, running, artifacts, true).primary[0].key).toBe('fmn_exec:artifact:execution.md')
    expect(summarizeProduced(board, [], [], false)).toEqual({ primary: [], others: [] })
  })
})
