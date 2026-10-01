import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))

const ports = { inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Result' }] }
type EndNode = { id: string; title: string; outcome: 'done' | 'rejected' }
type LimitNode = { id: string; title: string; target: string; rounds?: number; seconds?: number; warnSeconds?: number; tokens?: number }
export const board: {
  id: string; slug: string; title: string; rev: number; etag: string
  missions: Array<{ id: string; title: string; goal: string; beadId: string }>
  formations: Array<{ id: string; type: string; title: string; brief?: { goal: string }; inputs: Array<{ id: string; label: string }>; outputs: Array<{ id: string; label: string }>; slots: Array<Record<string, unknown>> }>
  gates: Array<{ id: string; title: string; kinds: string[]; criterion: string }>
  tools: unknown[]
  ends?: EndNode[]
  limits?: LimitNode[]
  connections: Array<{ id: string; from: string; to: string }>
} = {
  id: 'brd_browser', slug: 'browser', title: 'Peer and judge', rev: 1, etag: 'board-1',
  inputCards: [{ id: 'mission', title: 'Delivery', goal: 'Verify the implementation' }],
  formations: [
    { id: 'execution', type: 'orchestrated', title: 'Execution', brief: { goal: 'Implement the reviewed plan and verify the result.' }, ...ports,
      slots: [{ id: 'controller', label: 'Controller', agentId: 'claude', harness: 'claude-code', effort: 'medium', controller: true },
        { id: 'worker', label: 'Worker 1', agentId: 'codex', harness: 'openai-codex', effort: 'medium', controller: false }] },
    { id: 'peer', type: 'peer', title: 'Peer review', brief: { goal: 'Compare the design and implementation.' }, ...ports,
      slots: [{ id: 'peer_1', label: 'Reviewer', agentId: 'codex', harness: 'openai-codex', effort: 'medium', controller: false }] },
    { id: 'judge', type: 'solo', title: 'Judge', brief: { goal: 'Check the review evidence.' }, ...ports,
      slots: [{ id: 'judge_1', label: 'Judge', agentId: 'claude', harness: 'claude-code', effort: 'medium', controller: true }] },
  ],
  gates: [{ id: 'gate', title: 'Review gate', kinds: ['formation'], criterion: 'Evidence supports acceptance' },
    { id: 'loose', title: 'Disconnected gate', kinds: ['human'], criterion: 'Operator approval' }],
  tools: [],
  ends: [],
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
    harness: 'claude-code', effort: 'medium', controller: true, createdSeq: 7, sessionName: 'scratch-controller', state: 'live',
    columns: 96, rows: 30, terminalUrl: '/api/runs/run_browser/seats/7/terminal' },
  { runId: 'run_browser', nodeId: 'execution', nodeTitle: 'Execution', slotId: 'worker', slotLabel: 'Worker 1',
    harness: 'openai-codex', effort: 'medium', controller: false, createdSeq: 8, sessionName: 'scratch-worker', state: 'live',
    columns: 96, rows: 30, terminalUrl: '/api/runs/run_browser/seats/8/terminal' },
]

export const judgeBlockReason = 'invalid judge result: missing or unterminated archon-verdict block'
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
  '/api/runs/run_browser/evidence/nodes/execution': { evidence: { runId: 'run_browser', nodeId: 'execution', kind: 'formation',
    definition: { title: 'Execution', outputs: [{ id: 'out', label: 'Result' }], outgoing: [{ id: 'review', from: 'execution:out', to: 'gate:in' }] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 3, text: evidenceText('Implemented the reviewed plan.'), ports: [{ portId: 'out', text: evidenceText('Implemented the reviewed plan.\n\nAll tests pass.') }] } }] } },
  '/api/runs/run_browser/evidence/nodes/peer': { evidence: { runId: 'run_browser', nodeId: 'peer', kind: 'formation',
    definition: { title: 'Peer review', outputs: [{ id: 'out', label: 'Result' }], outgoing: [] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 6, text: evidenceText(''), ports: [{ portId: 'out', text: evidenceText(reviewMarkdown), ref: { artifact: 'review.md' } }] } }] } },
  '/api/runs/run_browser/evidence/artifacts': { artifacts: [{ name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z' }, { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z' }], truncated: false },
  '/api/runs/run_browser/evidence/artifacts/review.md': { artifact: { name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', text: evidenceText(reviewMarkdown) } },
  '/api/runs/run_browser/evidence/artifacts/logs/worker.log': { artifact: { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z', kind: 'text', text: evidenceText('worker started\nworker finished') } },
}

// The run is blocked before Execution's third run: its Limit card Cap allows 2
// rounds, both used (archon-o7p.8). It resumes only with one more round granted.
export const limitReason = 'Execution used 2 of 2 rounds'
const limitUse = { kind: 'rounds', limitId: 'lim_cap', nodeId: 'execution', used: 2, max: 2 }
const blockedAtLimitEvents = [
  { seq: 1, type: 'run_started' },
  { seq: 2, type: 'node_started', nodeId: 'execution', attempt: 1 },
  { seq: 3, type: 'node_output', nodeId: 'execution', status: 'done' },
  { seq: 4, type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: 'fail' },
  { seq: 5, type: 'node_started', nodeId: 'execution', attempt: 2 },
  { seq: 6, type: 'node_output', nodeId: 'execution', status: 'done' },
  { seq: 7, type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: 'fail' },
  { seq: 8, type: 'error', nodeId: 'execution' },
  { seq: 9, type: 'run_blocked', nodeId: 'execution' },
]

// Or Execution ran out of its 30 min (archon-o7p.8.2): its seat was warned with
// 5 min left, then the step stopped. A grant gives the card's 30 min again.
export const timeLimitReason = 'Execution used 30 min of 30 min'
const timeLimitUse = { kind: 'time', limitId: 'lim_cap', nodeId: 'execution', used: 1800, max: 1800 }
const blockedAtTimeEvents = [
  { seq: 1, type: 'run_started' },
  { seq: 2, type: 'node_started', nodeId: 'execution', attempt: 1 },
  { seq: 3, type: 'limit_warning', nodeId: 'execution', slotId: 'worker', attempt: 1 },
  { seq: 4, type: 'error', nodeId: 'execution' },
  { seq: 5, type: 'run_blocked', nodeId: 'execution' },
]

/** Whole seconds in words, as the server's durationWords. */
function durationWords(seconds: number): string {
  if (seconds < 60) return `${seconds} s`
  const parts = [Math.floor(seconds / 3600) && `${Math.floor(seconds / 3600)} h`, Math.floor(seconds % 3600 / 60) && `${Math.floor(seconds % 3600 / 60)} min`, seconds % 60 && `${seconds % 60} s`]
  return parts.filter(Boolean).join(' ')
}

/**
 * Limit cards' findings as board validation reports them (limitFindings):
 * invalid_limit errors and empty_limit warnings, each on the card.
 */
function limitFindings(current: typeof board) {
  const errors: Array<{ code: string; nodeId: string; message: string }> = []
  const warnings: typeof errors = []
  const named = (id: string) => [...current.inputCards, ...current.formations, ...current.gates, ...(current.limits || [])].find(node => node.id === id)?.title || id
  const covered = new Map<string, string>()
  for (const limit of current.limits || []) {
    const name = named(limit.id)
    if (!limit.target) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} is wired to nothing: wire it to a step, or to the Input card for the whole mission` })
    else if (covered.has(limit.target)) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `${named(limit.target)} has two Limit cards, ${named(covered.get(limit.target)!)} and ${name}: keep one` })
    else covered.set(limit.target, limit.id)
    if (limit.rounds !== undefined && limit.rounds <= 0) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} holds rounds = ${limit.rounds}: rounds must be a positive whole number` })
    if (limit.seconds !== undefined && limit.seconds <= 0) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} holds seconds = ${limit.seconds}: time must be a positive whole number of seconds` })
    const warn = limit.warnSeconds
    if (warn !== undefined && warn <= 0) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} holds warnSeconds = ${warn}: the warning must be a positive whole number of seconds` })
    else if (warn !== undefined && limit.seconds === undefined) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} warns with ${durationWords(warn)} left but sets no time: give it time, or clear the warning` })
    else if (warn !== undefined && limit.seconds! > 0 && warn >= limit.seconds!) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} warns with ${durationWords(warn)} left of ${durationWords(limit.seconds!)}, before any work: warn with less time left` })
    if (limit.tokens !== undefined && limit.tokens <= 0) errors.push({ code: 'invalid_limit', nodeId: limit.id, message: `Limit ${name} holds tokens = ${limit.tokens}: tokens must be a positive whole number` })
    if (limit.rounds === undefined && limit.seconds === undefined && limit.tokens === undefined) warnings.push({ code: 'empty_limit', nodeId: limit.id, message: `Limit ${name} sets no limit: give it rounds, time or tokens, or delete it` })
  }
  return { errors, warnings }
}

export async function cockpitFixture(page: Page, options: { far?: boolean; run?: boolean; blockedAtJudge?: boolean; blockedAtLimit?: boolean; blockedAtTime?: boolean; succeeded?: boolean; themeFailure?: boolean; waitingHuman?: boolean; secondGate?: boolean; join?: boolean; extraAgents?: number } = {}) {
  const currentBoard = structuredClone(board)
  // A time block is a limit block too, with a time card.
  const timeBlock = Boolean(options.blockedAtTime)
  if (timeBlock) options = { ...options, blockedAtLimit: true }
  if (options.blockedAtLimit) {
    // Execution's send-back route leads back to it, and its card allows 2 rounds, or 30 min warned at 5 min left.
    currentBoard.limits = [timeBlock ? { id: 'lim_cap', title: 'Cap', target: 'execution', seconds: 1800, warnSeconds: 300 } : { id: 'lim_cap', title: 'Cap', target: 'execution', rounds: 2 }]
    currentBoard.connections = [...currentBoard.connections, { id: 'gate-fail', from: 'gate:fail', to: 'execution:in' }]
  }
  // The run resumes once the grant is recorded.
  let granted = false
  const blockEvents = timeBlock ? blockedAtTimeEvents : blockedAtLimitEvents
  const blockReason = timeBlock ? timeLimitReason : limitReason
  const blockUse = timeBlock ? timeLimitUse : limitUse
  if (options.waitingHuman) {
    // The answered gate's routes lead somewhere, as admission requires (archon-o7p.10).
    currentBoard.ends = [{ id: 'end_done', title: 'Done', outcome: 'done' }, { id: 'end_rejected', title: 'Rejected', outcome: 'rejected' }]
    currentBoard.connections = [...currentBoard.connections,
      { id: 'loose-pass', from: 'loose:pass', to: 'end_done:in' }, { id: 'loose-fail', from: 'loose:fail', to: 'end_rejected:in' }]
  }
  // A second human gate waits at the same time (archon-o7p.11).
  if (options.secondGate) {
    currentBoard.gates = [...currentBoard.gates, { id: 'second', title: 'Second look', kinds: ['human'], criterion: 'A second operator look' }]
    currentBoard.connections = [...currentBoard.connections, { id: 'second-in', from: 'execution:out', to: 'second:in' },
      { id: 'second-pass', from: 'second:pass', to: 'end_done:in' }, { id: 'second-fail', from: 'second:fail', to: 'end_rejected:in' }]
  }
  if (options.join) {
    currentBoard.formations = ['a', 'b', 'c', 'sink'].map(id => ({ ...structuredClone(board.formations[2]), id, title: id === 'sink' ? 'Join' : `Solo ${id.toUpperCase()}` }))
    currentBoard.inputCards = []
    currentBoard.gates = []
    currentBoard.connections = []
  }
  const boardState = () => currentBoard
  let nodes = positions.map(p => ({ ...p, x: p.x + (options.far ? 1800 : 0) }))
  if (options.join) nodes = [{ id: 'a', x: 100, y: 80 }, { id: 'b', x: 100, y: 350 }, { id: 'c', x: 100, y: 620 }, { id: 'sink', x: 650, y: 350 }]
  if (options.waitingHuman) nodes = [...nodes, { id: 'end_done', x: 504, y: 672 }, { id: 'end_rejected', x: 504, y: 784 }]
  if (options.secondGate) nodes = [...nodes, { id: 'second', x: 112, y: 920 }]
  if (options.blockedAtLimit) nodes = [...nodes, { id: 'lim_cap', x: 448, y: 476 }]
  const endsOf = () => (currentBoard.ends ||= [])
  const limitsOf = () => (currentBoard.limits ||= [])
  // Mirrors the store (checkLimitWrite): a target must be a step or the Input card, and no knob negative.
  type Knobs = { rounds?: number; seconds?: number; warnSeconds?: number; tokens?: number }
  const limitRefusal = (target: string | undefined, knobs: Knobs) => {
    if (target && ![...currentBoard.inputCards, ...currentBoard.formations].some(node => node.id === target)) {
      return `"${target}" is not a step or the Input card; a Limit card covers one of them`
    }
    if (knobs.rounds !== undefined && knobs.rounds < 0) return 'rounds must be a positive whole number'
    if (knobs.seconds !== undefined && knobs.seconds < 0) return 'time must be a positive whole number of seconds'
    if (knobs.warnSeconds !== undefined && knobs.warnSeconds < 0) return 'the warning must be a positive whole number of seconds'
    if (knobs.tokens !== undefined && knobs.tokens < 0) return 'tokens must be a positive whole number'
    return ''
  }
  let seatsFetches = 0
  let themeFetches = 0
  const writes: string[] = []
  if (options.run || options.waitingHuman || options.blockedAtLimit) await page.addInitScript(() => localStorage.setItem('archon.activeRun.browser', 'run_browser'))
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    if (method !== 'GET') writes.push(`${method} ${path}`)
    // Once the mission holds Limit cards its writes carry its own ETag, so the
    // cockpit fetches validation again after each one, as against the daemon.
    const etag = () => path.startsWith('/api/missions/browser') && currentBoard.limits?.length ? currentBoard.etag : 'fixture-etag'
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data }, headers: { ETag: etag() } })
    const refuseLimit = (target: string | undefined, knobs: Knobs) => {
      const message = limitRefusal(target, knobs)
      return message ? route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_LIMIT', message } } }) : null
    }
    if (path === '/api/theme') {
      themeFetches++
      return route.fulfill(options.themeFailure ? { status: 500, json: { error: 'Unavailable' } } : { json: defaultTheme })
    }
    if (path === '/api/missions') return respond({ missions: [boardState()] })
    if (path.endsWith('/notes')) return respond({ notes: { schema: 2, missionId: board.id, rev: 1,
      mission: [{ id: 'nte_board', author: 'human:ui', createdAt: '2026-09-12T00:00:00Z', text: 'Keep the current graph and harness identities.' }],
      elements: [{ nodeId: 'execution', entries: [
        { id: 'nte_brief', author: 'human:ui', createdAt: '2026-09-12T00:00:00Z', text: 'Pair the controller with a builder.' },
        { id: 'nte_reply', author: 'agent:archon', createdAt: '2026-09-12T00:05:00Z', text: 'Controller directs the assigned worker.' },
      ] }], updatedAt: '2026-09-12T00:05:00Z', etag: 'notes-1' } })
    if (path.endsWith('/layout')) {
      if (method === 'PATCH') nodes = positions.map(p => ({ ...p }))
      return respond({ layout: { missionId: board.id, missionRev: 1, etag: 'layout-1', nodes, edges: [] } })
    }
    if (path === '/api/missions/browser') {
      if (method === 'PATCH') {
        const body = route.request().postDataJSON()
        const edit = body.wireConnection || body.rewireConnection
        if (edit) {
          const edges = currentBoard.connections.filter(edge => !(body.rewireConnection && edge.from === edit.from && edge.to === edit.previousTo))
          const reject = (code: string, message: string) => route.fulfill({ status: 409, json: { success: false, error: { code, message } } })
          if (edit.from.split(':')[0] === edit.to.split(':')[0]) return reject('SELF_WIRE', 'A node cannot be wired to itself')
          if (edges.some(edge => edge.from === edit.from && edge.to === edit.to)) return reject('DUPLICATE_CONNECTION', 'This connection already exists')
          let to = edit.to
          // Any number of routes may lead into one End node.
          const intoEnd = endsOf().some(end => `${end.id}:in` === to)
          if (!intoEnd && edges.some(edge => edge.to === to)) {
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
        } else if (body.createEnd) {
          // Mirrors the store (CreateEnd): done unless asked, titled after the outcome.
          const { outcome = 'done', title, x, y } = body.createEnd
          if (outcome !== 'done' && outcome !== 'rejected') {
            return route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_END_OUTCOME', message: `End outcome "${outcome}" must be done or rejected` } } })
          }
          const end: EndNode = { id: `end_${currentBoard.rev}`, title: title || (outcome === 'rejected' ? 'Rejected' : 'Done'), outcome }
          endsOf().push(end)
          nodes = [...nodes, { id: end.id, x, y }]
          currentBoard.rev++
          currentBoard.etag = `board-${currentBoard.rev}`
          return respond({ mission: boardState(), layout: { missionId: board.id, missionRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] }, end })
        } else if (body.updateEnd) {
          const end = endsOf().find(item => item.id === body.updateEnd.id)
          if (!end) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          if (body.updateEnd.title !== undefined) end.title = body.updateEnd.title
          if (body.updateEnd.outcome !== undefined) end.outcome = body.updateEnd.outcome
        } else if (body.deleteEnd) {
          const id = body.deleteEnd.id
          if (!endsOf().some(end => end.id === id)) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          currentBoard.ends = endsOf().filter(end => end.id !== id)
          currentBoard.connections = currentBoard.connections.filter(edge => edge.from.split(':')[0] !== id && edge.to.split(':')[0] !== id)
          nodes = nodes.filter(node => node.id !== id)
          currentBoard.rev++
          currentBoard.etag = `board-${currentBoard.rev}`
          return respond({ mission: boardState(), layout: { missionId: board.id, missionRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] }, endId: id })
        } else if (body.createLimit) {
          // Mirrors the store (CreateLimit): titled Limit unless named; a knob of 0 sets none.
          const { title, target = '', rounds = 0, seconds = 0, warnSeconds = 0, tokens = 0, x, y } = body.createLimit
          const refused = refuseLimit(target, { rounds, seconds, warnSeconds, tokens })
          if (refused) return refused
          const limit: LimitNode = { id: `lim_${currentBoard.rev}`, title: title || 'Limit', target, ...(rounds ? { rounds } : {}), ...(seconds ? { seconds } : {}), ...(warnSeconds ? { warnSeconds } : {}), ...(tokens ? { tokens } : {}) }
          limitsOf().push(limit)
          nodes = [...nodes, { id: limit.id, x, y }]
          currentBoard.rev++
          currentBoard.etag = `board-${currentBoard.rev}`
          return respond({ mission: boardState(), layout: { missionId: board.id, missionRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] }, limit })
        } else if (body.updateLimit) {
          // Mirrors the store (UpdateLimit): an empty target unwires, a knob of 0 clears it.
          const { id, title, target, ...knobs } = body.updateLimit
          const limit = limitsOf().find(item => item.id === id)
          if (!limit) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          const refused = refuseLimit(target, knobs)
          if (refused) return refused
          if (title !== undefined) limit.title = title || 'Limit'
          if (target !== undefined) limit.target = target
          for (const knob of ['rounds', 'seconds', 'warnSeconds', 'tokens'] as const) {
            if (knobs[knob] === 0) delete limit[knob]
            else if (knobs[knob] !== undefined) limit[knob] = knobs[knob]
          }
        } else if (body.deleteLimit) {
          const id = body.deleteLimit.id
          if (!limitsOf().some(limit => limit.id === id)) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          currentBoard.limits = limitsOf().filter(limit => limit.id !== id)
          nodes = nodes.filter(node => node.id !== id)
          currentBoard.rev++
          currentBoard.etag = `board-${currentBoard.rev}`
          return respond({ mission: boardState(), layout: { missionId: board.id, missionRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] }, limitId: id })
        } else if (body.deleteFormation || body.deleteGate || body.deleteInputCard) {
          // Mirrors the store: a delete drops the node, the connections touching it and its layout node.
          const id = (body.deleteFormation || body.deleteGate || body.deleteInputCard).id
          const key = body.deleteFormation ? 'formations' : body.deleteGate ? 'gates' : 'inputCards'
          const list = currentBoard[key] as Array<{ id: string }>
          if (!list.some(node => node.id === id)) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          ;(currentBoard[key] as Array<{ id: string }>) = list.filter(node => node.id !== id)
          currentBoard.connections = currentBoard.connections.filter(edge => edge.from.split(':')[0] !== id && edge.to.split(':')[0] !== id)
          nodes = nodes.filter(node => node.id !== id)
        } else if (body.restoreNode) {
          // Mirrors the store: a node ID already in the mission refuses the whole restore.
          const { inputCard, formation, gate, end, limit, connections, index, x, y } = body.restoreNode
          const node = inputCard || formation || gate || end || limit
          const key = inputCard ? 'inputCards' : formation ? 'formations' : gate ? 'gates' : end ? 'ends' : 'limits'
          endsOf()
          limitsOf()
          const taken = [...currentBoard.inputCards, ...currentBoard.formations, ...currentBoard.gates, ...endsOf(), ...limitsOf()].some(item => item.id === node.id)
          if (taken) return route.fulfill({ status: 409, json: { success: false, error: { code: 'INVALID_NODE_RESTORE', message: `node "${node.id}" is already in the mission` } } })
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
        if (body.deleteFormation || body.deleteGate || body.deleteInputCard || body.restoreNode) {
          return respond({ mission: boardState(), layout: { missionId: board.id, missionRev: currentBoard.rev, etag: `layout-${currentBoard.rev}`, nodes, edges: [] } })
        }
      }
      return respond({ mission: boardState() })
    }
    if (path.endsWith('/changes')) return respond({ signal: { changed: false } })
    // Validation is served once the mission holds Limit cards, so their findings mark them as drafts.
    if (path.endsWith('/validation') && currentBoard.limits?.length) return respond({ missionRev: currentBoard.rev, missionEtag: currentBoard.etag, ...limitFindings(currentBoard) })
    if (path === '/api/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/agents') return respond({ agents: [
      { id: 'claude', displayName: 'Claude controller', harnessDefault: 'claude-code', assignable: true, liveness: 'live', tags: [], kind: 'controller' },
      { id: 'codex', displayName: 'Codex builder', harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'builder' },
      // A large roster, as on a real host, makes long staffing menus.
      ...Array.from({ length: options.extraAgents || 0 }, (_, index) => ({ id: `agent-${index + 1}`, displayName: `Roster agent ${index + 1}`,
        harnessDefault: 'openai-codex', assignable: true, liveness: 'live', tags: [], kind: 'builder' })),
    ] })
    if (path === '/api/agents/codex') return respond({ id: 'codex', displayName: 'Codex builder', kind: 'builder', summary: 'Builds the change.', tags: [],
      harnessDefault: 'openai-codex', harnessVariants: [{ id: 'openai-codex', sessionStem: 'codex', launch: 'codex', effectiveEffort: 'medium',
        efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'],
        seatLaunch: `exec '/usr/local/bin/codex' -c 'model_reasoning_effort="medium"' -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox` }], etag: 'codex-card' })
    if (path === '/api/runs/run_browser' && options.waitingHuman) return respond({ runId: 'run_browser', status: 'waiting_human', final: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: options.secondGate ? 4 : 3, cwd: runCwd,
      waitingGates: [{ gateId: 'loose', requestedSeq: 3 }, ...(options.secondGate ? [{ gateId: 'second', requestedSeq: 4 }] : [])] })
    if (path === '/api/runs/run_browser' && options.succeeded) return respond({ runId: 'run_browser', status: 'succeeded', final: true, missionSlug: 'browser', inputCardId: 'mission', eventCount: 7, cwd: runCwd })
    if (options.succeeded && path in succeededEvidence) return respond(succeededEvidence[path])
    if (options.blockedAtLimit && path === '/api/runs/run_browser/resume' && method === 'POST') {
      // Mirrors the coordinator: a spent limit resumes only with a grant.
      const body = route.request().postDataJSON()
      if (!body.grant) return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'this run stopped at a spent limit: resume it with --grant to give one more round or the card\'s time again' } } })
      granted = true
      return respond({ runId: 'run_browser', status: 'running', final: false, resumeAllowed: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length + 2, cwd: runCwd })
    }
    if (path === '/api/runs/run_browser' && options.blockedAtLimit) return respond(granted
      ? { runId: 'run_browser', status: 'running', final: false, resumeAllowed: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length + 2, cwd: runCwd }
      : { runId: 'run_browser', status: 'blocked', final: false, resumeAllowed: true, resumePolicy: 'grant', missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length, cwd: runCwd })
    if (path.endsWith('/events') && options.blockedAtLimit) return respond({ events: granted
      ? [...blockEvents, { seq: blockEvents.length + 1, type: 'run_resumed' }, { seq: blockEvents.length + 2, type: 'node_started', nodeId: 'execution', attempt: timeBlock ? 2 : 3 }]
      : blockEvents })
    if (options.blockedAtLimit && path === '/api/runs/run_browser/evidence/problems') return respond({ problems: [
      { seq: blockEvents.length - 1, type: 'error', code: 'limit_reached', nodeIds: ['execution'], reason: { text: blockReason, bytes: blockReason.length }, limit: blockUse },
      { seq: blockEvents.length, type: 'run_blocked', code: 'limit_reached', nodeIds: ['execution'], reason: { text: blockReason, bytes: blockReason.length }, resumeAllowed: true, limit: blockUse, ...(granted ? { resumedSeq: blockEvents.length + 1 } : {}) },
    ] })
    if (path === '/api/runs/run_browser' && options.blockedAtJudge) return respond({ runId: 'run_browser', status: 'blocked', final: false, resumeAllowed: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: 8, cwd: runCwd })
    if (path === '/api/runs/run_browser') return respond({ runId: 'run_browser', status: 'running', final: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: 2, cwd: runCwd })
    if (path.endsWith('/events') && options.waitingHuman) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' },
      { seq: 3, type: 'human_input_requested', nodeId: 'loose', gateId: 'loose' },
      ...(options.secondGate ? [{ seq: 4, type: 'human_input_requested', nodeId: 'second', gateId: 'second' }] : [])] })
    if (path === '/api/runs/run_browser/gates/second/request') return respond({ request: { gateId: 'second', requestedSeq: 4, criterion: 'A second operator look',
      input: { fromNodeId: 'execution', fromPortId: 'out', truncated: false, text: 'Execution so far.' },
      routes: [{ verdict: 'pass', targets: [{ nodeId: 'end_done', title: 'Done', kind: 'end', outcome: 'done' }] },
        { verdict: 'fail', targets: [{ nodeId: 'end_rejected', title: 'Rejected', kind: 'end', outcome: 'rejected' }] }] } })
    if (path.endsWith('/events') && options.blockedAtJudge) return respond({ events: blockedAtJudgeEvents })
    if (path.endsWith('/events') && options.succeeded) return respond({ events: succeededEvents })
    if (path.endsWith('/events')) return respond({ events: [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_started', nodeId: 'execution' }] })
    if (path === '/api/runs/run_browser/gates/loose/request') return respond({ request: { gateId: 'loose', requestedSeq: 3, criterion: board.gates[1].criterion,
      input: { fromNodeId: 'execution', fromPortId: 'out', truncated: false, text: Array.from({ length: 60 }, (_, i) => `${i + 1}. A question the operator should answer before the brief is written.`).join('\n') },
      // The disconnected gate: as the daemon derives it while Execution is still open, each
      // verdict ends this path at an End node and the run goes on with its other work.
      routes: [{ verdict: 'pass', targets: [{ nodeId: 'end_done', title: 'Done', kind: 'end', outcome: 'done' }] },
        { verdict: 'fail', targets: [{ nodeId: 'end_rejected', title: 'Rejected', kind: 'end', outcome: 'rejected' }] }] } })
    if (options.blockedAtJudge && path === '/api/runs/run_browser/evidence/problems') return respond({ problems: [
      { seq: 7, type: 'error', code: 'invalid_judge_result', nodeIds: ['gate'], reason: { text: 'missing or unterminated archon-verdict block', bytes: 44 } },
      { seq: 8, type: 'run_blocked', nodeIds: ['gate'], reason: { text: judgeBlockReason, bytes: judgeBlockReason.length }, resumeAllowed: false },
    ] })
    if (path.endsWith('/escalations')) return respond({ escalations: [] })
    if (path.endsWith('/seats')) { seatsFetches++; return respond({ runId: 'run_browser', available: true, seats }) }
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  return { writes, board: boardState, seatsFetches: () => seatsFetches, themeFetches: () => themeFetches }
}
