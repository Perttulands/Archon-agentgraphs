import { type Page } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

/* The Wayfinding board with a writable mission: updateMission patches apply
 * and are recorded, so a spec can follow a human channel change and its undo.
 * Every other write is refused as in the Wayfinding fixture. */

export const missionId: string = wayfinding.board.missions[0].id

type Patch = { updateMission?: { id: string } & Record<string, unknown> }

export async function humanChannelFixture(page: Page, options: { humanChannel?: 'session' } = {}) {
  const fixture = await wayfindingFixture(page)
  let board = structuredClone(wayfinding.board)
  if (options.humanChannel) board.missions = board.missions.map((mission: { id: string }) => ({ ...mission, humanChannel: options.humanChannel }))
  const patches: Patch[] = []
  // Routes added later answer first, so this one serves the board and takes its mission patches.
  await page.route('**/api/formations/boards/wayfinding', async route => {
    const request = route.request()
    if (request.method() === 'GET') return route.fulfill({ json: { success: true, data: { board } }, headers: { ETag: board.etag } })
    const patch = request.postDataJSON() as Patch
    if (request.method() !== 'PATCH' || !patch.updateMission) return route.fallback()
    patches.push(patch)
    const { id, ...fields } = patch.updateMission
    board = {
      ...board, rev: board.rev + 1, etag: `wayfinding-${board.rev + 1}`,
      missions: board.missions.map((mission: { id: string }) => mission.id === id ? { ...mission, ...fields } : mission),
    }
    return route.fulfill({ json: { success: true, data: { board } }, headers: { ETag: board.etag } })
  })
  return { ...fixture, patches }
}

/** Saves a screenshot when ARCHON_SCREENSHOT_DIR names a directory, for evidence outside the test run. */
export async function evidenceShot(page: Page, name: string) {
  const dir = process.env.ARCHON_SCREENSHOT_DIR
  if (dir) await page.screenshot({ path: `${dir}/${name}.png` })
}

const byTitle = (title: string) => {
  const node = [...wayfinding.board.formations, ...wayfinding.board.gates].find((item: { title: string }) => item.title === title)
  if (!node) throw new Error(`Wayfinding has no ${title}`)
  return node as { id: string; slots?: Array<{ id: string; label: string; agentId: string; harness: string }> }
}
export const peers = byTitle('Question peers')
export const answerGate = byTitle('Answer questions')
export const framingGate = byTitle('Framing review')
const territory = byTitle('Map the territory')
export const talkRunId = 'run_talk'
const requestedSeq = 11

export const talkAgents = [
  { id: 'delivery-planner', displayName: 'Delivery Planner', harnessDefault: 'claude-code', assignable: true, liveness: 'live', tags: [], kind: 'planner' },
  { id: 'codex-planner', displayName: 'Codex Planner', harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'planner' },
]

/**
 * A session-channel Wayfinding run waiting at Answer questions. The gate asked
 * both Question peers seats, which are kept on call; with `fallbackReason` the
 * ask fell back to a notification instead. Framing review was decided earlier,
 * relayed by the Map the territory seat.
 */
export async function talkRunFixture(page: Page, options: { fallbackReason?: string } = {}) {
  const fixture = await humanChannelFixture(page, { humanChannel: 'session' })
  await page.addInitScript(runId => localStorage.setItem('chrote-formations-active-run-wayfinding', runId), talkRunId)
  const [first, second] = peers.slots!
  const asked = options.fallbackReason ? [] : [
    { nodeId: peers.id, slotId: first.id, createdSeq: 21, deliveredSeq: 12 },
    { nodeId: peers.id, slotId: second.id, createdSeq: 22, deliveredSeq: 13 },
  ]
  const waitingOn = [{ gateId: answerGate.id, requestedSeq }]
  let run = {
    runId: talkRunId, status: 'waiting_human', final: false, boardSlug: 'wayfinding', missionId, eventCount: 13, humanChannel: 'session',
    waitingGates: [{ gateId: answerGate.id, requestedSeq, askedSeats: asked, ...(options.fallbackReason ? { fallbackReason: options.fallbackReason } : {}) }],
    onCallSeats: asked.map(seat => ({ nodeId: seat.nodeId, slotId: seat.slotId, createdSeq: seat.createdSeq, keptSeq: 9, waitingOn })),
  }
  let events = [
    { seq: 1, type: 'run_started' },
    { seq: 2, type: 'node_started', nodeId: territory.id, attempt: 1 },
    { seq: 3, type: 'node_output', nodeId: territory.id, status: 'done' },
    { seq: 4, type: 'human_input_requested', nodeId: framingGate.id, gateId: framingGate.id },
    { seq: 5, type: 'human_verdict_recorded', nodeId: framingGate.id, gateId: framingGate.id, verdict: 'pass' },
    { seq: 6, type: 'gate_verdict', nodeId: framingGate.id, gateId: framingGate.id, verdict: 'pass' },
    { seq: 7, type: 'node_started', nodeId: peers.id, attempt: 1 },
    { seq: 8, type: 'node_output', nodeId: peers.id, status: 'done' },
    { seq: 9, type: 'seat_cleanup', nodeId: peers.id, slotId: first.id, outcome: 'kept_on_call' },
    { seq: 10, type: 'seat_cleanup', nodeId: peers.id, slotId: second.id, outcome: 'kept_on_call' },
    { seq: requestedSeq, type: 'human_input_requested', nodeId: answerGate.id, gateId: answerGate.id },
  ]
  const seat = (slot: typeof first, createdSeq: number) => ({
    runId: talkRunId, nodeId: peers.id, nodeTitle: 'Question peers', slotId: slot.id, slotLabel: slot.label, harness: slot.harness,
    controller: false, createdSeq, sessionName: `form-${talkRunId}-${slot.id}`, state: 'live', columns: 100, rows: 30,
    terminalUrl: `/api/formations/runs/${talkRunId}/seats/${createdSeq}/terminal`, onCall: { keptSeq: 9, waitingOn },
  })
  const text = (value: string) => ({ text: value, bytes: value.length })
  const respond = (data: unknown) => ({ json: { success: true, data }, headers: { ETag: 'fixture-etag' } })
  await page.route('**/api/agents', route => route.fulfill(respond({ agents: talkAgents, count: talkAgents.length })))
  await page.route('**/api/formations/runs**', route => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() !== 'GET') return route.fallback()
    if (path === '/api/formations/runs') return route.fulfill(respond([run]))
    if (path === `/api/formations/runs/${talkRunId}`) return route.fulfill(respond(run))
    if (path === `/api/formations/runs/${talkRunId}/events`) return route.fulfill(respond({ events }))
    if (path === `/api/formations/runs/${talkRunId}/gates/${answerGate.id}/request`) return route.fulfill(respond({ request: {
      gateId: answerGate.id, requestedSeq, criterion: '',
      input: { fromNodeId: peers.id, text: '1. Who reads the brief first?\n2. Which sources are off limits?\n3. What does done look like?', truncated: false },
    } }))
    if (path === `/api/formations/runs/${talkRunId}/escalations`) return route.fulfill(respond({ escalations: [] }))
    if (path === `/api/formations/runs/${talkRunId}/seats`) return route.fulfill(respond({ runId: talkRunId, available: true, seats: [seat(first, 21), seat(second, 22)] }))
    if (path === `/api/formations/runs/${talkRunId}/evidence/nodes/${framingGate.id}`) return route.fulfill(respond({ evidence: {
      runId: talkRunId, nodeId: framingGate.id, kind: 'gate',
      evaluations: [{ seq: 4, kinds: ['human'], criterion: text('Pick a framing'), kindResults: [],
        humanRequests: [{ seq: 4, pending: false, decision: { seq: 5, verdict: 'pass', response: text('Framing 2, as we discussed.'), decidedBy: 'human:operator', relayedBy: territory.slots![0].id } }],
        verdict: { seq: 5, verdict: 'pass', reason: text('Framing 2, as we discussed.'), routePort: 'pass', evidence: [] } }],
    } }))
    return route.fallback()
  })

  // Each seat's socket answers the handshake with a prompt, echoes typing as a terminal does, and records every frame it receives.
  const frames = new Map<number, string[]>()
  await page.routeWebSocket('**/seats/*/terminal', socket => {
    const createdSeq = Number(socket.url().match(/seats\/(\d+)\/terminal/)![1])
    // A seat's frames from every socket to it, such as a talk window's and a Peek's.
    const received = frames.get(createdSeq) || []
    frames.set(createdSeq, received)
    socket.onMessage(data => {
      const value = Buffer.isBuffer(data) ? data.toString() : data
      received.push(value)
      if (value.startsWith('{')) socket.send(Buffer.from(`0Question peers, seat ${createdSeq}: which questions should we settle first?\r\n> `))
      if (value.startsWith('0')) socket.send(Buffer.from(`0${value.slice(1).replace(/\x1b/g, '').replace(/\r/g, '\r\n> ')}`))
    })
  })
  const typed = (createdSeq: number) => (frames.get(createdSeq) || []).filter(frame => frame.startsWith('0')).map(frame => frame.slice(1)).join('')
  const resizes = (createdSeq: number) => (frames.get(createdSeq) || []).filter(frame => frame.startsWith('1')).map(frame => JSON.parse(frame.slice(1)) as { columns: number; rows: number })
  /** The operator's decision is recorded, as a seat's relayed command would: the request stops waiting, and the kept seats hold no ask until they close. */
  const decide = () => {
    events = [...events, { seq: 14, type: 'human_verdict_recorded', nodeId: answerGate.id, gateId: answerGate.id }]
    run = { ...run, status: 'running', eventCount: 14, waitingGates: [], onCallSeats: run.onCallSeats.map(seat => ({ ...seat, waitingOn: [] })) }
  }
  return { ...fixture, typed, resizes, decide }
}
