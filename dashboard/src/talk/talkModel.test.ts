import { describe, expect, it } from 'vitest'
import type { BoardDocument, RunStatusProjection } from '../components/formationsTypes'
import { askingFormationTitle, gateTalk, talkSeatStatus } from './talkModel'

const ports = { inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Output' }] }
const board = {
  id: 'brd', slug: 'b', title: 'B', rev: 1, etag: 'e', missions: [], tools: [],
  formations: [
    { id: 'peers', type: 'peer', title: 'Question peers', ...ports, slots: [
      { id: 'slot_a', label: 'Peer', controller: false, agentId: 'delivery-planner', harness: 'claude-code' },
      { id: 'slot_b', label: 'Peer', controller: false, agentId: 'codex-planner', harness: 'openai-codex' },
    ] },
    { id: 'draft', type: 'solo', title: 'Draft the brief', ...ports, slots: [{ id: 'slot_c', label: 'Agent', controller: true, agentId: 'delivery-planner', harness: 'claude-code' }] },
  ],
  gates: [
    { id: 'answers', title: 'Answer questions', kinds: ['human'], criterion: '' },
    { id: 'review', title: 'Review', kinds: ['formation'], criterion: '' },
    { id: 'signoff', title: 'Sign-off', kinds: ['human'], criterion: '' },
  ],
  connections: [
    { id: 'c1', from: 'peers:out', to: 'answers:in' },
    { id: 'c2', from: 'draft:out', to: 'review:in' },
    { id: 'c3', from: 'review:pass', to: 'signoff:in' },
  ],
} as unknown as BoardDocument
const agents = [{ id: 'delivery-planner', displayName: 'Delivery Planner', assignable: true }]
const run = (waiting: NonNullable<RunStatusProjection['waitingGates']>[number], humanChannel?: 'notify' | 'session'): RunStatusProjection => ({
  runId: 'run_1', status: 'waiting_human', final: false, boardSlug: 'b', missionId: 'm', eventCount: 9, humanChannel, waitingGates: [waiting],
})

describe('talking with the agents a gate asked', () => {
  it('names each asked peer by agent and harness, and the formation they belong to', () => {
    const talk = gateTalk(board, run({ gateId: 'answers', requestedSeq: 9, askedSeats: [
      { nodeId: 'peers', slotId: 'slot_a', createdSeq: 5, deliveredSeq: 12 },
      { nodeId: 'peers', slotId: 'slot_b', createdSeq: 6, deliveredSeq: 13 },
    ] }, 'session'), agents, { gateId: 'answers', requestedSeq: 9 })
    expect(talk?.formationTitle).toBe('Question peers')
    expect(talk?.fallbackReason).toBe('')
    expect(talk?.seats.map(seat => [seat.windowId, seat.label])).toEqual([
      ['talk:run_1:5', 'Delivery Planner · Claude Code'],
      ['talk:run_1:6', 'codex-planner · Codex'],
    ])
  })

  it('names the asking formation before any seat receives the ask, through a judge gate', () => {
    expect(gateTalk(board, run({ gateId: 'signoff', requestedSeq: 20, askedSeats: [] }, 'session'), agents, { gateId: 'signoff', requestedSeq: 20 }))
      .toEqual({ formationTitle: 'Draft the brief', seats: [], fallbackReason: '' })
    expect(askingFormationTitle(board, 'answers')).toBe('Question peers')
  })

  it('keeps a fallback reason, and offers nothing on a notify run, a final run or another request', () => {
    const fallback = run({ gateId: 'answers', requestedSeq: 9, askedSeats: [], fallbackReason: 'the lab executor keeps no seats' }, 'session')
    expect(gateTalk(board, fallback, agents, { gateId: 'answers', requestedSeq: 9 })?.fallbackReason).toBe('the lab executor keeps no seats')
    expect(gateTalk(board, run({ gateId: 'answers', requestedSeq: 9 }), agents, { gateId: 'answers', requestedSeq: 9 })).toBeNull()
    expect(gateTalk(board, { ...fallback, final: true }, agents, { gateId: 'answers', requestedSeq: 9 })).toBeNull()
    expect(gateTalk(board, fallback, agents, { gateId: 'answers', requestedSeq: 8 })).toBeNull()
  })

  it('follows an open seat from waiting for the operator to past the decision, even when the decision ends the run', () => {
    const asked = { nodeId: 'peers', slotId: 'slot_a', createdSeq: 5, deliveredSeq: 12 }
    const waiting = { ...run({ gateId: 'answers', requestedSeq: 9, askedSeats: [asked] }, 'session'),
      onCallSeats: [{ nodeId: 'peers', slotId: 'slot_a', createdSeq: 5, keptSeq: 8, waitingOn: [{ gateId: 'answers', requestedSeq: 9 }] }] }
    const seat = gateTalk(board, waiting, agents, { gateId: 'answers', requestedSeq: 9 })!.seats[0]
    const requested = [{ seq: 9, type: 'human_input_requested', runId: 'run_1', gateId: 'answers' }]
    const recorded = [...requested, { seq: 15, type: 'human_verdict_recorded', runId: 'run_1', gateId: 'answers' }]
    expect(talkSeatStatus(waiting, requested, seat)).toBe('waiting')
    const resumed = { ...waiting, status: 'running', waitingGates: [], onCallSeats: [{ ...waiting.onCallSeats[0], waitingOn: [] }] }
    expect(talkSeatStatus(resumed, recorded, seat)).toBe('decided')
    // The gate was the last step: the seat ended before the run succeeded.
    expect(talkSeatStatus({ ...resumed, status: 'succeeded', final: true, onCallSeats: [] }, recorded, seat)).toBe('decided')
    // An earlier verdict on the same gate is not this request's.
    expect(talkSeatStatus(waiting, [{ seq: 4, type: 'human_verdict_recorded', runId: 'run_1', gateId: 'answers' }, ...requested], seat)).toBe('waiting')
    // Canceled while waiting, seats lost while the request waits, or a daemon without the projection say nothing.
    expect(talkSeatStatus({ ...waiting, status: 'canceled', final: true, waitingGates: [], onCallSeats: [] }, requested, seat)).toBeNull()
    expect(talkSeatStatus({ ...waiting, onCallSeats: [] }, requested, seat)).toBeNull()
    expect(talkSeatStatus({ ...waiting, onCallSeats: undefined }, requested, seat)).toBeNull()
  })
})
