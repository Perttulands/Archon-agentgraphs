import type { RunEvent, RunStatusProjection, RunStatusResult } from './formationsTypes'

export const runEventTypes = [
  'run_started',
  'run_resumed',
  'node_waiting',
  'node_started',
  'slot_dispatch',
  'slot_result',
  'node_output',
  'gate_evaluating',
  'gate_verdict',
  'human_input_requested',
  'human_verdict_recorded',
  'verification_verdict',
  'escalation_raised',
  'error',
  'run_blocked',
  'run_canceled',
  'run_failed',
  'run_succeeded',
]

export function upsertRunEvent(events: RunEvent[], next: RunEvent): RunEvent[] {
  if (!next.runId || !next.seq) return events
  const existing = events.findIndex(event => event.seq === next.seq && event.runId === next.runId)
  const merged = existing >= 0
    ? events.map((event, index) => index === existing ? next : event)
    : [...events, next]
  return merged.sort((a, b) => a.seq - b.seq)
}

export function statusFromRunEvent(event: RunEvent): string {
  switch (event.type) {
    case 'run_started':
    case 'run_resumed':
      return 'running'
    case 'human_input_requested':
      return 'waiting_human'
    case 'run_blocked':
      return 'blocked'
    case 'run_canceled':
      return 'canceled'
    case 'run_failed':
      return 'failed'
    case 'run_succeeded':
      return 'succeeded'
    default:
      return ''
  }
}

export function runEventResumeAllowed(event: RunEvent, fallback: boolean): boolean {
  if (event.type === 'run_blocked') return event.data?.resumeAllowed === true
  if (event.type === 'run_started' || event.type === 'run_resumed') return false
  if (event.type === 'run_canceled' || event.type === 'run_failed' || event.type === 'run_succeeded') return false
  return fallback
}

export function runEventText(event: RunEvent): string {
  const data = event.data || {}
  if (typeof data.text === 'string') return data.text
  if (typeof data.reason === 'string') return data.reason
  if (typeof data.prompt === 'string') return data.prompt
  if (typeof data.error === 'string') return data.error
  return ''
}

export function runStatusFromResponse(data: RunStatusProjection | RunStatusResult): RunStatusProjection {
  const nested = (data as RunStatusResult).status
  return typeof nested === 'object' && nested !== null ? nested : data as RunStatusProjection
}

export function runEventReportRef(event: RunEvent): string {
  const reportRef = event.data?.reportRef
  return typeof reportRef === 'string' ? reportRef : ''
}

export function activeRunStorageKey(slug: string): string {
  return `chrote-formations-active-run-${slug}`
}

export type NodeRunState = '' | 'running' | 'done' | 'blocked' | 'waiting' | 'failed'

interface RunProjection {
  states: Map<string, NodeRunState>
  /** The sequence at which each node last changed state. */
  changedAt: Map<string, number>
  /** The latest attempt recorded for each node. */
  attempts: Map<string, number>
  gates: Set<string>
}

function projectRun(events: RunEvent[]): RunProjection {
  const states = new Map<string, NodeRunState>()
  const changedAt = new Map<string, number>()
  const attempts = new Map<string, number>()
  const gates = new Set<string>()
  const set = (nodeId: string, state: NodeRunState, seq: number) => {
    states.set(nodeId, state)
    changedAt.set(nodeId, seq)
  }
  // A block holds the node only until the run resumes. Recording a human verdict
  // blocks on its gate and resumes at once, so the gate returns to its verdict.
  const beforeBlock = new Map<string, NodeRunState>()
  for (const event of [...events].sort((a, b) => a.seq - b.seq)) {
    if (event.type === 'run_resumed') {
      for (const [blockedId, prior] of beforeBlock) set(blockedId, prior, event.seq)
      beforeBlock.clear()
      continue
    }
    const nodeId = event.nodeId || event.gateId
    if (!nodeId) {
      // A block or failure that names no node stops whatever was in flight.
      if (event.type === 'run_blocked' || event.type === 'run_failed') {
        for (const [inFlight, state] of states) {
          if (state !== 'running') continue
          if (event.type === 'run_blocked' && !beforeBlock.has(inFlight)) beforeBlock.set(inFlight, state)
          set(inFlight, event.type === 'run_blocked' ? 'blocked' : 'failed', event.seq)
        }
      }
      continue
    }
    if (event.gateId) gates.add(event.gateId)
    if (event.attempt && (event.type === 'node_started' || event.type === 'slot_dispatch')) attempts.set(nodeId, event.attempt)
    switch (event.type) {
      case 'node_started':
      case 'slot_dispatch':
      case 'gate_evaluating':
        set(nodeId, 'running', event.seq)
        break
      case 'human_input_requested':
      case 'node_waiting':
        set(nodeId, 'waiting', event.seq)
        break
      case 'node_output': {
        const status = typeof event.data?.status === 'string' ? event.data.status : 'done'
        set(nodeId, status === 'blocked' ? 'blocked' : 'done', event.seq)
        break
      }
      case 'gate_verdict': {
        const verdict = typeof event.data?.verdict === 'string' ? event.data.verdict : ''
        set(nodeId, verdict === 'fail' ? 'failed' : 'done', event.seq)
        break
      }
      case 'run_blocked':
        if (!beforeBlock.has(nodeId)) beforeBlock.set(nodeId, states.get(nodeId) || '')
        set(nodeId, 'blocked', event.seq)
        break
      case 'run_failed':
        set(nodeId, 'failed', event.seq)
        break
      default:
        break
    }
  }
  return { states, changedAt, attempts, gates }
}

/** Project honest per-node run state from the ledger events (mirrors the engine vocabulary). */
export function projectNodeStates(events: RunEvent[], _activeRun?: RunStatusProjection | null): Map<string, NodeRunState> {
  return projectRun(events).states
}

export type RunPointKind = 'waiting' | 'running' | 'blocked' | 'failed'

/** Where a run is now: the node it waits at, runs, or stopped on. */
export interface RunPoint {
  kind: RunPointKind
  /** Empty when the ledger names no node. */
  nodeId: string
  gate: boolean
  /** The running node's attempt, when recorded. */
  attempt?: number
  /** The run_blocked event whose recorded reason explains a block. */
  blockSeq?: number
}

export function runCurrentPoint(events: RunEvent[], run: RunStatusProjection | null): RunPoint | null {
  if (!run) return null
  const { states, changedAt, attempts, gates } = projectRun(events)
  // The node most recently put into this state is where the run is.
  const latest = (state: NodeRunState) => {
    let found = ''
    for (const [nodeId, current] of states) {
      if (current === state && (!found || (changedAt.get(nodeId) || 0) > (changedAt.get(found) || 0))) found = nodeId
    }
    return found
  }
  if (run.status === 'waiting_human') {
    const gateId = run.waitingGates?.[0]?.gateId || openHumanGateId(events)
    return gateId ? { kind: 'waiting', nodeId: gateId, gate: true } : null
  }
  if (run.status === 'blocked') {
    const block = [...events].sort((a, b) => b.seq - a.seq).find(event => event.type === 'run_blocked')
    const nodeId = block?.gateId || block?.nodeId || latest('blocked')
    return { kind: 'blocked', nodeId, gate: gates.has(nodeId), blockSeq: block?.seq }
  }
  if (run.status === 'failed') {
    const nodeId = latest('failed')
    return { kind: 'failed', nodeId, gate: gates.has(nodeId) }
  }
  if (run.status === 'running') {
    const nodeId = latest('running')
    return nodeId ? { kind: 'running', nodeId, gate: gates.has(nodeId), attempt: attempts.get(nodeId) } : null
  }
  return null
}

/**
 * The run bar's words for a run point. `reason` is the block's recorded reason;
 * a pause is the block that follows the operator's answer until the run resumes.
 */
export function runPointPhrase(point: RunPoint, title: string, reason = '', pause = false): string {
  const where = title || point.nodeId
  switch (point.kind) {
    case 'waiting':
      return `waiting for you at ${where}`
    case 'running':
      if (point.gate) return `evaluating ${where}`
      return `running ${where}${point.attempt && point.attempt > 1 ? ` (attempt ${point.attempt})` : ''}`
    case 'blocked':
      if (pause) return `paused at ${where} after your answer`
      return `${where ? `blocked at ${where}` : 'blocked'}${reason ? `: ${reason}` : ''}`
    case 'failed':
      return where ? `failed at ${where}` : ''
  }
}

export function openHumanGateId(events: RunEvent[]): string {
  let openGateId = ''
  for (const event of [...events].sort((a, b) => a.seq - b.seq)) {
    if (event.type === 'human_input_requested' && event.gateId) {
      openGateId = event.gateId
      continue
    }
    if ((event.type === 'human_verdict_recorded' || event.type === 'gate_verdict') && event.gateId === openGateId) {
      openGateId = ''
    }
    if (event.type === 'run_succeeded' || event.type === 'run_failed' || event.type === 'run_canceled') {
      openGateId = ''
    }
  }
  return openGateId
}
