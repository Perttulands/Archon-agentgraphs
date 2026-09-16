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
  '/api/formations/runs/run_browser/evidence/nodes/execution': { evidence: { runId: 'run_browser', nodeId: 'execution', kind: 'formation', attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 3, text: evidenceText('Implemented the reviewed plan.'), ports: [{ portId: 'out', text: evidenceText('Implemented the reviewed plan.\n\nAll tests pass.') }] } }] } },
  '/api/formations/runs/run_browser/evidence/nodes/peer': { evidence: { runId: 'run_browser', nodeId: 'peer', kind: 'formation', attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 6, text: evidenceText(''), ports: [{ portId: 'out', text: evidenceText(reviewMarkdown), ref: { artifact: 'review.md' } }] } }] } },
  '/api/formations/runs/run_browser/evidence/artifacts': { artifacts: [{ name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z' }, { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z' }], truncated: false },
  '/api/formations/runs/run_browser/evidence/artifacts/review.md': { artifact: { name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', text: evidenceText(reviewMarkdown) } },
  '/api/formations/runs/run_browser/evidence/artifacts/logs/worker.log': { artifact: { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z', kind: 'text', text: evidenceText('worker started\nworker finished') } },
}

export async function cockpitFixture(page: Page, options: { far?: boolean; run?: boolean; blockedAtJudge?: boolean; succeeded?: boolean; themeFailure?: boolean; waitingHuman?: boolean } = {}) {
  let nodes = positions.map(p => ({ ...p, x: p.x + (options.far ? 1800 : 0) }))
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
    if (path === '/api/formations/boards') return respond({ boards: [board] })
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
    if (path === '/api/formations/boards/browser') return respond({ board })
    if (path.endsWith('/changes')) return respond({ signal: { changed: false } })
    if (path === '/api/formations/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/agents') return respond({ agents: [
      { id: 'claude', displayName: 'Claude controller', harnessDefault: 'claude-code', assignable: true, liveness: 'live', tags: [], kind: 'controller' },
      { id: 'codex', displayName: 'Codex builder', harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'builder' },
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
  return { writes, seatsFetches: () => seatsFetches, themeFetches: () => themeFetches }
}
