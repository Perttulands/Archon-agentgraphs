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
  const seats = (waiting.askedSeats || []).map(asked => talkSeat(board, run.runId, agents, asked))
  const formationTitle = seats[0]?.formationTitle || askingFormationTitle(board, gate.gateId)
  return { formationTitle, seats, fallbackReason }
}

function talkSeat(board: BoardDocument | null | undefined, runId: string, agents: readonly AgentProjection[], asked: AskedSeat): TalkSeat {
  const formation = board?.formations.find(node => node.id === asked.nodeId)
  const slot = formation?.slots.find(item => item.id === asked.slotId)
  const persona = slot?.agentId ? agents.find(agent => agent.id === slot.agentId) : undefined
  const agent = persona?.displayName || slot?.agentId || slot?.label || asked.slotId
  const harness = slot?.harness || persona?.harnessDefault || ''
  return {
    windowId: talkSeatWindowId(runId, asked.createdSeq),
    runId,
    createdSeq: asked.createdSeq,
    nodeId: asked.nodeId,
    formationTitle: formation?.title || asked.nodeId,
    slotLabel: slot?.label || asked.slotId,
    agent,
    harness,
    label: [agent, harnessName(harness)].filter(Boolean).join(' · '),
  }
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
