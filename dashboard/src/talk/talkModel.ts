import { harnessName } from '../components/formationsCockpitVisuals'
import type { AgentProjection, AskedSeat, BoardDocument, RunStatusProjection } from '../components/formationsTypes'

/**
 * Talking with the agents a human gate asked (ADR-0019): on a session-channel
 * run, a waiting gate's ask goes to the seats of the formation whose work it
 * judges, and the operator answers with them in those seats' terminals.
 */

export interface TalkSeat {
  /** The seat's window: one per run and seat. */
  windowId: string
  runId: string
  createdSeq: number
  /** The request the seat was asked, which the window was opened to talk through. */
  gateId: string
  requestedSeq: number
  nodeId: string
  formationTitle: string
  slotLabel: string
  agent: string
  harness: string
  /** Agent and harness, which tell peers on one formation apart: "Delivery Planner · Claude Code". */
  label: string
}

export interface GateTalk {
  /** The asking formation, as the Talk button names it. */
  formationTitle: string
  seats: TalkSeat[]
  /** Why the ask went to the notify command instead; the operator answers in the panel. */
  fallbackReason: string
}

export function talkSeatWindowId(runId: string, createdSeq: number): string {
  return `talk:${runId}:${createdSeq}`
}

/** What a waiting human gate offers for talking, or null on a notify run or another gate. */
export function gateTalk(
  board: BoardDocument | null | undefined,
  run: RunStatusProjection | null | undefined,
  agents: readonly AgentProjection[],
  gate: { gateId: string; requestedSeq: number } | null | undefined,
): GateTalk | null {
  if (!run || run.final || !gate) return null
  const waiting = run.waitingGates?.find(item => item.gateId === gate.gateId && item.requestedSeq === gate.requestedSeq)
  if (!waiting) return null
  const fallbackReason = waiting.fallbackReason || ''
  if (run.humanChannel !== 'session' && !fallbackReason) return null
  const seats = (waiting.askedSeats || []).map(asked => talkSeat(board, run.runId, agents, gate, asked))
  const formationTitle = seats[0]?.formationTitle || askingFormationTitle(board, gate.gateId)
  return { formationTitle, seats, fallbackReason }
}

function talkSeat(
  board: BoardDocument | null | undefined, runId: string, agents: readonly AgentProjection[],
  gate: { gateId: string; requestedSeq: number }, asked: AskedSeat,
): TalkSeat {
  const formation = board?.formations.find(node => node.id === asked.nodeId)
  const slot = formation?.slots.find(item => item.id === asked.slotId)
  const persona = slot?.agentId ? agents.find(agent => agent.id === slot.agentId) : undefined
  const agent = persona?.displayName || slot?.agentId || slot?.label || asked.slotId
  const harness = slot?.harness || persona?.harnessDefault || ''
  return {
    windowId: talkSeatWindowId(runId, asked.createdSeq),
    runId,
    createdSeq: asked.createdSeq,
    gateId: gate.gateId,
    requestedSeq: gate.requestedSeq,
    nodeId: asked.nodeId,
    formationTitle: formation?.title || asked.nodeId,
    slotLabel: slot?.label || asked.slotId,
    agent,
    harness,
    label: [agent, harnessName(harness)].filter(Boolean).join(' · '),
  }
}

/**
 * Where an open talk seat stands: still holding an ask for the operator, or
 * past it because the request it was asked is no longer waiting while the run
 * goes on, so the agent closes once idle. Null when the run tells neither, such
 * as a run that ended, or asked seats lost while the request still waits.
 */
export function talkSeatStatus(run: RunStatusProjection | null | undefined, seat: TalkSeat): 'waiting' | 'decided' | null {
  if (!run || run.final || run.runId !== seat.runId || !run.onCallSeats) return null
  if (run.onCallSeats.some(kept => kept.createdSeq === seat.createdSeq && kept.waitingOn.length)) return 'waiting'
  const pending = run.waitingGates?.some(gate => gate.gateId === seat.gateId && gate.requestedSeq === seat.requestedSeq)
  return pending ? null : 'decided'
}

/** The nearest formation behind a gate's input, following that input back through gates. */
export function askingFormationTitle(board: BoardDocument | null | undefined, gateId: string): string {
  const gates = new Set((board?.gates || []).map(gate => gate.id))
  const seen = new Set<string>()
  let target = gateId
  while (board && !seen.has(target)) {
    seen.add(target)
    const feed = board.connections.find(connection => connection.to === `${target}:in`)
    const from = feed?.from.split(':')[0]
    if (!from) return ''
    if (!gates.has(from)) return board.formations.find(node => node.id === from)?.title || ''
    target = from
  }
  return ''
}
