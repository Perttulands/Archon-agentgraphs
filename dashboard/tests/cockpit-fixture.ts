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

export async function cockpitFixture(page: Page, options: { far?: boolean; run?: boolean; themeFailure?: boolean; waitingHuman?: boolean } = {}) {
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
    if (path === '/api/formations/runs/run_browser') return respond({ runId: 'run_browser', status: 'running', final: false, boardSlug: 'browser', missionId: 'mission', eventCount: 2, cwd: runCwd })
    if (path.endsWith('/events') && options.waitingHuman) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' },
      { seq: 3, type: 'human_input_requested', nodeId: 'loose', gateId: 'loose' }] })
    if (path.endsWith('/events')) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' }] })
    if (path === '/api/formations/runs/run_browser/gates/loose/request') return respond({ request: { gateId: 'loose', requestedSeq: 3, criterion: board.gates[1].criterion,
      input: { fromNodeId: 'execution', fromPortId: 'out', truncated: false, text: Array.from({ length: 60 }, (_, i) => `${i + 1}. A question the operator should answer before the brief is written.`).join('\n') } } })
    if (path.endsWith('/escalations')) return respond({ escalations: [] })
    if (path.endsWith('/seats')) { seatsFetches++; return respond({ runId: 'run_browser', available: true, seats }) }
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  return { writes, seatsFetches: () => seatsFetches, themeFetches: () => themeFetches }
}
