import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))

const ports = { inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Result' }] }
export const board = {
  id: 'brd_browser', slug: 'browser', title: 'Peer and judge', rev: 1, etag: 'board-1',
  missions: [{ id: 'mission', title: 'Delivery', goal: 'Verify the implementation', beadId: 'form-mnf' }],
  formations: [
    { id: 'execution', type: 'orchestrated', title: 'Execution', brief: { goal: 'Implement the reviewed plan and verify the result.' }, ...ports,
      slots: [{ id: 'controller', label: 'Controller', agentId: 'claude', harness: 'claude-code', controller: true },
        { id: 'worker', label: 'Worker 1', agentId: 'codex', harness: 'openai-codex', controller: false }] },
    { id: 'peer', type: 'peer', title: 'Peer review', brief: { goal: 'Compare the design and implementation.' }, ...ports,
      slots: [{ id: 'peer_1', label: 'Reviewer', agentId: 'codex', harness: 'openai-codex', controller: false }] },
    { id: 'judge', type: 'solo', title: 'Judge', brief: { goal: 'Check the review evidence.' }, ...ports,
      slots: [{ id: 'judge_1', label: 'Judge', agentId: 'claude', harness: 'claude-code', controller: true }] },
  ],
  gates: [{ id: 'gate', title: 'Review gate', kinds: ['formation'], criterion: 'Evidence supports acceptance' },
    { id: 'loose', title: 'Disconnected gate', kinds: ['human'], criterion: 'Operator approval' }],
  connections: [
    { id: 'start', from: 'mission:out', to: 'execution:in' },
    { id: 'review', from: 'execution:out', to: 'gate:in' },
    { id: 'judge-send', from: 'gate:judge', to: 'judge:in' },
    { id: 'judge-return', from: 'judge:out', to: 'gate:judge' },
    { id: 'pass', from: 'gate:pass', to: 'peer:in' },
  ],
}
export const runCwd = '/srv/scratch/archon-browser-fixture/a/deliberately/long/working/directory/for/the/run'
const positions = [
  { id: 'mission', x: 112, y: 112 }, { id: 'execution', x: 448, y: 112 },
  { id: 'peer', x: 1120, y: 112 }, { id: 'judge', x: 784, y: 504 },
  { id: 'gate', x: 784, y: 112 }, { id: 'loose', x: 112, y: 700 },
]
export const seats = [
  { runId: 'run_browser', nodeId: 'execution', nodeTitle: 'Execution', slotId: 'controller', slotLabel: 'Controller',
    harness: 'claude-code', controller: true, createdSeq: 7, sessionName: 'scratch-controller', state: 'live',
    columns: 96, rows: 30, terminalUrl: '/api/formations/runs/run_browser/seats/7/terminal' },
  { runId: 'run_browser', nodeId: 'execution', nodeTitle: 'Execution', slotId: 'worker', slotLabel: 'Worker 1',
    harness: 'openai-codex', controller: false, createdSeq: 8, sessionName: 'scratch-worker', state: 'live',
    columns: 96, rows: 30, terminalUrl: '/api/formations/runs/run_browser/seats/8/terminal' },
]

export const judgeBlockReason = 'invalid judge result: missing or unterminated chrote-verdict block'
// The run is blocked at the review gate after its judge returned no verdict block.
const blockedAtJudgeEvents = [
  { seq: 1, type: 'run_started' },
  { seq: 2, type: 'node_started', nodeId: 'execution', attempt: 1 },
  { seq: 3, type: 'node_output', nodeId: 'execution', status: 'done' },
  { seq: 4, type: 'gate_evaluating', nodeId: 'gate', gateId: 'gate' },
  { seq: 5, type: 'node_started', nodeId: 'judge', attempt: 1 },
  { seq: 6, type: 'node_output', nodeId: 'judge', status: 'done' },
  { seq: 7, type: 'judge_attempt_failed', nodeId: 'gate', gateId: 'gate' },
  { seq: 8, type: 'run_blocked', nodeId: 'gate', gateId: 'gate' },
]

// The run succeeded: Execution returned its result as text, and Peer review, the
// last step, wrote review.md. A worker log is in the artifacts too.
const succeededEvents = [
  { seq: 1, type: 'run_started' },
  { seq: 2, type: 'node_started', nodeId: 'execution', attempt: 1 },
  { seq: 3, type: 'node_output', nodeId: 'execution', status: 'done' },
  { seq: 4, type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: 'pass' },
  { seq: 5, type: 'node_started', nodeId: 'peer', attempt: 1 },
  { seq: 6, type: 'node_output', nodeId: 'peer', status: 'done' },
  { seq: 7, type: 'run_succeeded' },
]
const evidenceText = (text: string) => ({ text, bytes: text.length })
export const reviewMarkdown = '# Peer review\n\nVerdict: **revise**. The design and the implementation disagree on retries.\n\n- Keep the retry budget\n- Log the second failure'
const succeededEvidence: Record<string, unknown> = {
  '/api/formations/runs/run_browser/evidence/nodes/execution': { evidence: { runId: 'run_browser', nodeId: 'execution', kind: 'formation',
    definition: { title: 'Execution', outputs: [{ id: 'out', label: 'Result' }], outgoing: [{ id: 'review', from: 'execution:out', to: 'gate:in' }] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 3, text: evidenceText('Implemented the reviewed plan.'), ports: [{ portId: 'out', text: evidenceText('Implemented the reviewed plan.\n\nAll tests pass.') }] } }] } },
  '/api/formations/runs/run_browser/evidence/nodes/peer': { evidence: { runId: 'run_browser', nodeId: 'peer', kind: 'formation',
    definition: { title: 'Peer review', outputs: [{ id: 'out', label: 'Result' }], outgoing: [] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 6, text: evidenceText(''), ports: [{ portId: 'out', text: evidenceText(reviewMarkdown), ref: { artifact: 'review.md' } }] } }] } },
  '/api/formations/runs/run_browser/evidence/artifacts': { artifacts: [{ name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z' }, { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z' }], truncated: false },
  '/api/formations/runs/run_browser/evidence/artifacts/review.md': { artifact: { name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', text: evidenceText(reviewMarkdown) } },
  '/api/formations/runs/run_browser/evidence/artifacts/logs/worker.log': { artifact: { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z', kind: 'text', text: evidenceText('worker started\nworker finished') } },
}

export async function cockpitFixture(page: Page, options: { far?: boolean; run?: boolean; blockedAtJudge?: boolean; succeeded?: boolean; themeFailure?: boolean; waitingHuman?: boolean; join?: boolean; extraAgents?: number } = {}) {
  const currentBoard = structuredClone(board)
  if (options.join) {
    currentBoard.formations = ['a', 'b', 'c', 'sink'].map(id => ({ ...structuredClone(board.formations[2]), id, title: id === 'sink' ? 'Join' : `Solo ${id.toUpperCase()}` }))
    currentBoard.missions = []
    currentBoard.gates = []
    currentBoard.connections = []
  }
  const boardState = () => currentBoard
  let nodes = positions.map(p => ({ ...p, x: p.x + (options.far ? 1800 : 0) }))
  if (options.join) nodes = [{ id: 'a', x: 100, y: 80 }, { id: 'b', x: 100, y: 350 }, { id: 'c', x: 100, y: 620 }, { id: 'sink', x: 650, y: 350 }]
  let seatsFetches = 0
  let themeFetches = 0
  const writes: string[] = []
  if (options.run || options.waitingHuman) await page.addInitScript(() => localStorage.setItem('chrote-formations-active-run-browser', 'run_browser'))
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    if (method !== 'GET') writes.push(`${method} ${path}`)
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data }, headers: { ETag: 'fixture-etag' } })
    if (path === '/api/theme') {
      themeFetches++
      return route.fulfill(options.themeFailure ? { status: 500, json: { error: 'Unavailable' } } : { json: defaultTheme })
    }
    if (path === '/api/formations/boards') return respond({ boards: [boardState()] })
    if (path.endsWith('/notes')) return respond({ notes: { schema: 2, boardId: board.id, rev: 1,
      board: [{ id: 'nte_board', author: 'human:ui', createdAt: '2026-09-12T00:00:00Z', text: 'Keep the current graph and harness identities.' }],
      elements: [{ nodeId: 'execution', entries: [
        { id: 'nte_brief', author: 'human:ui', createdAt: '2026-09-12T00:00:00Z', text: 'Pair the controller with a builder.' },
        { id: 'nte_reply', author: 'agent:archon', createdAt: '2026-09-12T00:05:00Z', text: 'Controller directs the assigned worker.' },
      ] }], updatedAt: '2026-09-12T00:05:00Z', etag: 'notes-1' } })
    if (path.endsWith('/layout')) {
      if (method === 'PATCH') nodes = positions.map(p => ({ ...p }))
      return respond({ layout: { boardId: board.id, boardRev: 1, etag: 'layout-1', nodes, edges: [] } })
    }
    if (path === '/api/formations/boards/browser') {
      if (method === 'PATCH') {
        const body = route.request().postDataJSON()
        const edit = body.wireConnection || body.rewireConnection
        if (edit) {
          const edges = currentBoard.connections.filter(edge => !(body.rewireConnection && edge.from === edit.from && edge.to === edit.previousTo))
          const reject = (code: string, message: string) => route.fulfill({ status: 409, json: { success: false, error: { code, message } } })
          if (edit.from.split(':')[0] === edit.to.split(':')[0]) return reject('SELF_WIRE', 'A node cannot be wired to itself')
          if (edges.some(edge => edge.from === edit.from && edge.to === edit.to)) return reject('DUPLICATE_CONNECTION', 'This connection already exists')
          let to = edit.to
          if (edges.some(edge => edge.to === to)) {
            const formation = currentBoard.formations.find(item => item.id === to.split(':')[0])
            if (!edit.joinIfOccupied || !formation) return reject('INPUT_OCCUPIED', 'Input already has a feed')
            const port = { id: `join_${currentBoard.rev}`, label: 'Input' }
            formation.inputs.push(port)
            to = `${formation.id}:${port.id}`
          }
          if (edit.removePreviousInput) {
            const [nodeId, portId] = edit.previousTo.split(':')
            if (edges.some(edge => edge.to === edit.previousTo || edge.from === edit.previousTo)) return reject('INPUT_OCCUPIED', 'Input already has a feed')
            const formation = currentBoard.formations.find(item => item.id === nodeId)!
            formation.inputs = formation.inputs.filter(port => port.id !== portId)
          }
          currentBoard.connections = [...edges, { id: `edge_${currentBoard.rev}`, from: edit.from, to }]
        } else if (body.deleteFormation || body.deleteGate || body.deleteMission) {
          // Mirrors the store: a delete drops the node, the connections touching it and its layout node.
          const id = (body.deleteFormation || body.deleteGate || body.deleteMission).id
          const key = body.deleteFormation ? 'formations' : body.deleteGate ? 'gates' : 'missions'
          const list = currentBoard[key] as Array<{ id: string }>
          if (!list.some(node => node.id === id)) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          ;(currentBoard[key] as Array<{ id: string }>) = list.filter(node => node.id !== id)
          currentBoard.connections = currentBoard.connections.filter(edge => edge.from.split(':')[0] !== id && edge.to.split(':')[0] !== id)
          nodes = nodes.filter(node => node.id !== id)
        } else if (body.restoreNode) {
          // Mirrors the store: a node ID already on the board refuses the whole restore.
          const { mission, formation, gate, connections, index, x, y } = body.restoreNode
          const node = mission || formation || gate
          const key = mission ? 'missions' : formation ? 'formations' : 'gates'
          const taken = [...currentBoard.missions, ...currentBoard.formations, ...currentBoard.gates].some(item => item.id === node.id)
          if (taken) return route.fulfill({ status: 409, json: { success: false, error: { code: 'INVALID_NODE_RESTORE', message: `node "${node.id}" is already on the board` } } })
          ;(currentBoard[key] as unknown[]).splice(index ?? (currentBoard[key] as unknown[]).length, 0, node)
          currentBoard.connections = [...currentBoard.connections, ...connections]
          nodes = [...nodes, { id: node.id, x, y }]
        } else if (body.addPort) {
          // Mirrors the store (AddFormationPort): the only directions are input and output.
          const { formationId, direction, label } = body.addPort
          if (direction !== 'input' && direction !== 'output') {
            return route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_PORT_DIRECTION', message: `port direction "${direction}" must be input or output` } } })
          }
          const formation = currentBoard.formations.find(item => item.id === formationId)
          if (!formation) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          const port = { id: `port_${currentBoard.rev}`, label: label || (direction === 'input' ? 'Input' : 'Output') }
          if (direction === 'input') formation.inputs = [...formation.inputs, port]
          else formation.outputs = [...formation.outputs, port]
        } else if (body.removePort) {
          const { formationId, portId } = body.removePort
          const formation = currentBoard.formations.find(item => item.id === formationId)
          if (!formation || ![...formation.inputs, ...formation.outputs].some(port => port.id === portId)) {
            return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          }
          formation.inputs = formation.inputs.filter(port => port.id !== portId)
          formation.outputs = formation.outputs.filter(port => port.id !== portId)
          currentBoard.connections = currentBoard.connections.filter(edge => edge.to !== `${formationId}:${portId}` && edge.from !== `${formationId}:${portId}`)
        } else if (body.unwireConnection) {
          currentBoard.connections = currentBoard.connections.filter(edge => edge.from !== body.unwireConnection.from || edge.to !== body.unwireConnection.to)
        } else {
          return route.fulfill({ status: 400, json: { success: false, error: { message: 'Unsupported fixture edit' } } })
        }
        currentBoard.rev++
        currentBoard.etag = `board-${currentBoard.rev}`
        // Node deletes and restores publish the layout with the board, as the store does.
        if (body.deleteFormation || body.deleteGate || body.deleteMission || body.restoreNode) {
          return respond({ board: boardState(), layout: { boardId: board.id, boardRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] } })
        }
      }
      return respond({ board: boardState() })
    }
    if (path.endsWith('/changes')) return respond({ signal: { changed: false } })
    if (path === '/api/formations/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/agents') return respond({ agents: [
      { id: 'claude', displayName: 'Claude controller', harnessDefault: 'claude-code', assignable: true, liveness: 'live', tags: [], kind: 'controller' },
      { id: 'codex', displayName: 'Codex builder', harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'builder' },
      // A large roster, as on a real host, makes long staffing menus.
      ...Array.from({ length: options.extraAgents || 0 }, (_, index) => ({ id: `agent-${index + 1}`, displayName: `Roster agent ${index + 1}`,
        harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'builder' })),
    ] })
    if (path === '/api/agents/codex') return respond({ id: 'codex', displayName: 'Codex builder', kind: 'builder', summary: 'Builds the change.', tags: [],
      harnessDefault: 'openai-codex', harnessVariants: [{ id: 'openai-codex', sessionStem: 'codex', launch: 'codex' }], etag: 'codex-card' })
    if (path === '/api/formations/runs/run_browser' && options.waitingHuman) return respond({ runId: 'run_browser', status: 'waiting_human', final: false, boardSlug: 'browser', missionId: 'mission', eventCount: 3, cwd: runCwd, waitingGates: [{ gateId: 'loose', requestedSeq: 3 }] })
    if (path === '/api/formations/runs/run_browser' && options.succeeded) return respond({ runId: 'run_browser', status: 'succeeded', final: true, boardSlug: 'browser', missionId: 'mission', eventCount: 7, cwd: runCwd })
    if (options.succeeded && path in succeededEvidence) return respond(succeededEvidence[path])
    if (path === '/api/formations/runs/run_browser' && options.blockedAtJudge) return respond({ runId: 'run_browser', status: 'blocked', final: false, resumeAllowed: false, boardSlug: 'browser', missionId: 'mission', eventCount: 8, cwd: runCwd })
    if (path === '/api/formations/runs/run_browser') return respond({ runId: 'run_browser', status: 'running', final: false, boardSlug: 'browser', missionId: 'mission', eventCount: 2, cwd: runCwd })
    if (path.endsWith('/events') && options.waitingHuman) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' },
      { seq: 3, type: 'human_input_requested', nodeId: 'loose', gateId: 'loose' }] })
    if (path.endsWith('/events') && options.blockedAtJudge) return respond({ events: blockedAtJudgeEvents })
    if (path.endsWith('/events') && options.succeeded) return respond({ events: succeededEvents })
    if (path.endsWith('/events')) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' }] })
    if (path === '/api/formations/runs/run_browser/gates/loose/request') return respond({ request: { gateId: 'loose', requestedSeq: 3, criterion: board.gates[1].criterion,
      input: { fromNodeId: 'execution', fromPortId: 'out', truncated: false, text: Array.from({ length: 60 }, (_, i) => `${i + 1}. A question the operator should answer before the brief is written.`).join('\n') } } })
    if (options.blockedAtJudge && path === '/api/formations/runs/run_browser/evidence/problems') return respond({ problems: [
      { seq: 7, type: 'error', code: 'invalid_judge_result', nodeIds: ['gate'], reason: { text: 'missing or unterminated chrote-verdict block', bytes: 44 } },
      { seq: 8, type: 'run_blocked', nodeIds: ['gate'], reason: { text: judgeBlockReason, bytes: judgeBlockReason.length }, resumeAllowed: false },
    ] })
    if (path.endsWith('/escalations')) return respond({ escalations: [] })
    if (path.endsWith('/seats')) { seatsFetches++; return respond({ runId: 'run_browser', available: true, seats }) }
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  return { writes, board: boardState, seatsFetches: () => seatsFetches, themeFetches: () => themeFetches }
}
