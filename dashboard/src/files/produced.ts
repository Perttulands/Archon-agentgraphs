import { useEffect, useRef, useState } from 'react'
import type { RunEvent } from '../components/formationsTypes'
import { fetchNodeEvidence, fetchRunArtifacts, type NodeEvidence, type RunArtifactEntry } from '../evidence/runEvidenceApi'

// What a run produced, read from the evidence routes (ADR-0017): each step's
// latest output, as the artifact files its ports name and the text it
// returned, plus the run's other artifact files.

export interface ProducedItem {
  /** Unique within the run. */
  key: string
  /** The step that produced it; absent for an artifact no step's output names. */
  nodeId?: string
  kind: 'artifact' | 'output'
  /** The artifact's name relative to the run's artifact directory. */
  artifact?: string
  /** The output port; absent for the seat's report. */
  portId?: string
  bytes: number
}

export interface NodeProduced {
  nodeId: string
  /** The sequence of the output this was read from. */
  seq: number
  definition?: NodeEvidence['definition']
  items: ProducedItem[]
}

/**
 * A step's latest output: the artifacts its ports name, the text of ports
 * that name none, and the seat's report when no port already says the same.
 */
export function producedFromEvidence(evidence: NodeEvidence): NodeProduced | null {
  if (evidence.kind !== 'formation' && evidence.kind !== 'tool') return null
  const output = [...(evidence.attempts || [])].reverse().find(attempt => attempt.output)?.output
  if (!output) return null
  const nodeId = evidence.nodeId
  const items: ProducedItem[] = []
  for (const port of output.ports) {
    const artifact = port.ref?.artifact
    if (artifact) {
      if (!items.some(item => item.artifact === artifact)) {
        items.push({ key: `${nodeId}:artifact:${artifact}`, nodeId, kind: 'artifact', artifact, portId: port.portId, bytes: port.text.bytes })
      }
    } else if (port.text.bytes) {
      items.push({ key: `${nodeId}:port:${port.portId}`, nodeId, kind: 'output', portId: port.portId, bytes: port.text.bytes })
    }
  }
  const report = output.text
  if (report.bytes && !output.ports.some(port => port.text.bytes === report.bytes && port.text.text === report.text)) {
    items.push({ key: `${nodeId}:report`, nodeId, kind: 'output', bytes: report.bytes })
  }
  return { nodeId, seq: output.seq, definition: evidence.definition, items }
}

/** The latest output sequence of each node, from the run's events. */
export function latestOutputSeqs(events: readonly RunEvent[]): Map<string, number> {
  const seqs = new Map<string, number>()
  for (const event of events) {
    if (event.type === 'node_output' && event.nodeId && event.seq > (seqs.get(event.nodeId) || 0)) seqs.set(event.nodeId, event.seq)
  }
  return seqs
}

export interface RunProducedSummary {
  /** What the run bar shows first: a finished run's final outputs, or a running run's latest. */
  primary: ProducedItem[]
  /** Everything else the run produced, latest step first, then unclaimed artifact files. */
  others: ProducedItem[]
}

const artifactsFirst = (items: ProducedItem[]) => [...items].sort((a, b) => (a.kind === b.kind ? 0 : a.kind === 'artifact' ? -1 : 1))

export function summarizeProduced(
  produced: readonly NodeProduced[],
  artifacts: readonly RunArtifactEntry[],
  final: boolean,
): RunProducedSummary {
  // A judge answers its gate; its verdict is not what the run delivers.
  const judge = (step: NodeProduced) => step.definition?.outgoing.some(connection => connection.to.endsWith(':judge'))
  const steps = [...produced].filter(step => step.items.length).sort((a, b) => b.seq - a.seq)
  const work = steps.filter(step => !judge(step))
  const sinks = work.filter(step => step.definition?.outgoing.length === 0)
  const lead = final && sinks.length ? sinks : [work[0] || steps[0]].filter(Boolean)
  const primary = artifactsFirst(lead.flatMap(step => step.items))
  const claimed = new Set(steps.flatMap(step => step.items).map(item => item.artifact).filter(Boolean))
  const others = [
    ...steps.filter(step => !lead.includes(step)).flatMap(step => artifactsFirst(step.items)),
    ...artifacts.filter(entry => !claimed.has(entry.name))
      .map((entry): ProducedItem => ({ key: `artifact:${entry.name}`, kind: 'artifact', artifact: entry.name, bytes: entry.size })),
  ]
  return { primary, others }
}

/** Names are frozen with the run; older evidence falls back to stable IDs. */
export function producedNames(produced: readonly NodeProduced[]) {
  const definitions = new Map(produced.map(step => [step.nodeId, step.definition]))
  return {
    node: (nodeId: string) => definitions.get(nodeId)?.title || nodeId,
    port: (nodeId: string, portId: string) => definitions.get(nodeId)?.outputs.find(port => port.id === portId)?.label || portId,
  }
}

export interface RunProduced {
  produced: NodeProduced[]
  artifacts: RunArtifactEntry[]
}

const NOTHING_PRODUCED: RunProduced = { produced: [], artifacts: [] }

/**
 * Read what a run's steps produced. A step's evidence is read again only when
 * it records a newer output; the artifact list when any step does, and once
 * more when the run ends.
 */
export function useRunProduced(runId: string, events: readonly RunEvent[], final: boolean): RunProduced {
  const [state, setState] = useState<RunProduced & { runId: string }>({ runId: '', produced: [], artifacts: [] })
  const cache = useRef({ runId: '', nodes: new Map<string, NodeProduced>() })
  const outputs = [...latestOutputSeqs(events)]
  const outputKey = outputs.map(([nodeId, seq]) => `${nodeId}@${seq}`).sort().join(',')

  useEffect(() => {
    if (!runId) return
    if (cache.current.runId !== runId) cache.current = { runId, nodes: new Map() }
    const nodes = cache.current.nodes
    let current = true
    const wanted = outputKey ? outputKey.split(',').map(entry => {
      const at = entry.lastIndexOf('@')
      return { nodeId: entry.slice(0, at), seq: Number(entry.slice(at + 1)) }
    }) : []
    const stale = wanted.filter(({ nodeId, seq }) => nodes.get(nodeId)?.seq !== seq)
    void Promise.all([
      Promise.all(stale.map(({ nodeId }) => fetchNodeEvidence(runId, nodeId).then(producedFromEvidence, () => null))),
      fetchRunArtifacts(runId).catch(() => null),
    ]).then(([fresh, listed]) => {
      if (!current) return
      for (const step of fresh) if (step) nodes.set(step.nodeId, step)
      const kept = wanted.map(({ nodeId }) => nodes.get(nodeId)).filter((step): step is NodeProduced => Boolean(step))
      setState(previous => ({
        runId,
        produced: kept,
        artifacts: listed ? listed.artifacts : previous.runId === runId ? previous.artifacts : [],
      }))
    })
    return () => { current = false }
  }, [runId, outputKey, final])

  return state.runId === runId ? state : NOTHING_PRODUCED
}
