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
  return `archon.activeRun.${slug}`
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
  // A block holds the node only until the run resumes.
  const beforeBlock = new Map<string, NodeRunState>()
  // A gate answered while the run is blocked keeps waiting until the resume
  // routes the answer.
  let blocked = false
  const answeredWhileBlocked = new Set<string>()
  for (const event of [...events].sort((a, b) => a.seq - b.seq)) {
    if (event.type === 'run_resumed') {
      for (const [blockedId, prior] of beforeBlock) set(blockedId, prior, event.seq)
      beforeBlock.clear()
      for (const gateId of answeredWhileBlocked) set(gateId, 'running', event.seq)
      answeredWhileBlocked.clear()
      blocked = false
      continue
    }
    if (event.type === 'run_blocked') blocked = true
    // A finished run names the End nodes its paths reached (archon-o7p.10); the
    // rejected one a failure names turns failed below.
    if (event.type === 'run_succeeded' || event.type === 'run_failed') {
      const endIds: unknown[] = Array.isArray(event.data?.endIds) ? event.data.endIds : []
      for (const endId of endIds) if (typeof endId === 'string') set(endId, 'done', event.seq)
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
      // The operator answered; the run routes the verdict between steps, or
      // once it resumes.
      case 'human_verdict_recorded':
        if (blocked) answeredWhileBlocked.add(nodeId)
        else set(nodeId, 'running', event.seq)
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

/** The latest attempt recorded for each node, for the Flow view's status column. */
export function projectNodeAttempts(events: RunEvent[]): Map<string, number> {
  return projectRun(events).attempts
}

export type RunPointKind = 'waiting' | 'running' | 'blocked' | 'failed' | 'canceled'

/** Where a run is now: a node it waits at, runs or stopped on. */
export interface RunPoint {
  kind: RunPointKind
  /** Empty when the ledger names no node. */
  nodeId: string
  gate: boolean
  /** The running node's attempt, when recorded. */
  attempt?: number
  /** The run_blocked event whose recorded reason explains a block. */
  blockSeq?: number
  /** A running gate whose operator verdict the run is about to route. */
  answered?: boolean
}

/**
 * Every place a run is now, the one that most needs the operator first: each
 * gate waiting for them, then each step and gate running, as several can be
 * at once while a gate waits (archon-o7p.11). A blocked run names its block
 * first and the gates still waiting after it; a failed or canceled run names
 * where it stopped.
 */
export function runCurrentPoints(events: RunEvent[], run: RunStatusProjection | null): RunPoint[] {
  if (!run) return []
  const { states, changedAt, attempts, gates } = projectRun(events)
  const sorted = [...events].sort((a, b) => a.seq - b.seq)
  // The node most recently put into this state is where the run is.
  const latest = (state: NodeRunState) => {
    let found = ''
    for (const [nodeId, current] of states) {
      if (current === state && (!found || (changedAt.get(nodeId) || 0) > (changedAt.get(found) || 0))) found = nodeId
    }
    return found
  }
  const waitingIds = run.waitingGates?.length ? run.waitingGates.map(gate => gate.gateId) : openHumanGateIds(sorted)
  const waiting = waitingIds.map((nodeId): RunPoint => ({ kind: 'waiting', nodeId, gate: true }))
  const answered = new Set<string>()
  for (const event of sorted) {
    const gateId = event.gateId || event.nodeId || ''
    if (event.type === 'human_verdict_recorded') answered.add(gateId)
    if (event.type === 'gate_verdict' || event.type === 'gate_evaluating') answered.delete(gateId)
  }
  const running = [...states]
    .filter(([nodeId, state]) => state === 'running' && !waitingIds.includes(nodeId))
    .sort(([a], [b]) => (changedAt.get(a) || 0) - (changedAt.get(b) || 0))
    .map(([nodeId]): RunPoint => {
      const point: RunPoint = { kind: 'running', nodeId, gate: gates.has(nodeId), attempt: attempts.get(nodeId) }
      if (answered.has(nodeId)) point.answered = true
      return point
    })
  switch (run.status) {
    case 'waiting_human':
    case 'running':
      return [...waiting, ...running]
    case 'blocked': {
      const block = [...sorted].reverse().find(event => event.type === 'run_blocked')
      const nodeId = block?.gateId || block?.nodeId || latest('blocked')
      return [{ kind: 'blocked', nodeId, gate: gates.has(nodeId), blockSeq: block?.seq }, ...waiting.filter(point => point.nodeId !== nodeId)]
    }
    case 'failed': {
      const nodeId = latest('failed')
      return [{ kind: 'failed', nodeId, gate: gates.has(nodeId) }]
    }
    case 'canceled': {
      // A cancel names no node: it stopped the gate waiting for an answer, else the node in flight.
      const nodeId = openHumanGateIds(sorted.filter(event => event.type !== 'run_canceled'))[0] || latest('running') || latest('blocked')
      return [{ kind: 'canceled', nodeId, gate: gates.has(nodeId) }]
    }
    default:
      return []
  }
}

/** The one place that most needs the operator now (see runCurrentPoints). */
export function runCurrentPoint(events: RunEvent[], run: RunStatusProjection | null): RunPoint | null {
  return runCurrentPoints(events, run)[0] ?? null
}

/** The run bar's words for a run point. `reason` is the block's recorded reason. */
export function runPointPhrase(point: RunPoint, title: string, reason = ''): string {
  const where = title || point.nodeId
  switch (point.kind) {
    case 'waiting':
      return `waiting for you at ${where}`
    case 'running':
      if (point.answered) return `routing your answer at ${where}`
      if (point.gate) return `evaluating ${where}`
      return `running ${where}${point.attempt && point.attempt > 1 ? ` (attempt ${point.attempt})` : ''}`
    case 'blocked':
      return `${where ? `blocked at ${where}` : 'blocked'}${reason ? `: ${reason}` : ''}`
    case 'failed':
      return where ? `failed at ${where}` : ''
    case 'canceled':
      return where ? `canceled at ${where}` : ''
  }
}

/**
 * The gates waiting for a verdict, oldest first, by the daemon's rule: a
 * request waits until a verdict on its gate, a newer evaluation of that gate,
 * or the run's end. Several can wait at once.
 */
export function openHumanGateIds(events: RunEvent[]): string[] {
  const open: { gateId: string; seq: number }[] = []
  for (const event of [...events].sort((a, b) => a.seq - b.seq)) {
    const gateId = event.gateId || event.nodeId || ''
    switch (event.type) {
      case 'human_input_requested': {
        const existing = open.findIndex(item => item.gateId === gateId)
        if (existing >= 0) open.splice(existing, 1)
        open.push({ gateId, seq: event.seq })
        break
      }
      case 'human_verdict_recorded':
      case 'gate_evaluating': {
        const existing = open.findIndex(item => item.gateId === gateId)
        if (existing >= 0) open.splice(existing, 1)
        break
      }
      case 'run_succeeded':
      case 'run_failed':
      case 'run_canceled':
        open.length = 0
        break
    }
  }
  return open.map(item => item.gateId)
}
