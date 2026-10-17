import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { assignedSettings, harnesses, rosterAnswer } from './roster-terms'
const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))

const ports = { inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Result' }] }
type Port = { id: string; label: string; kind?: string }
type MissionOutput = { name: string; kind: string; description?: string; required?: boolean; from?: string }
type EndNode = { id: string; title: string; outcome: 'done' | 'rejected' }
type LimitNode = { id: string; title: string; target: string; rounds?: number; seconds?: number; warnSeconds?: number; tokens?: number }
export const board: {
  id: string; slug: string; title: string; rev: number; etag: string
  inputCards: Array<{ id: string; title: string; goal: string; outputs?: MissionOutput[] } & Record<string, unknown>>
  formations: Array<{ id: string; type: string; title: string; brief?: { goal: string }; inputs: Port[]; outputs: Port[]; slots: Array<Record<string, unknown>> }>
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
export const flowInputs = [{ name: 'subject', kind: 'text', value: 'A character seen from behind and in conversation. '.repeat(12) + 'Keep the final detail.' }, { name: 'facts', kind: 'file', value: '/workspace/identity.md' }, { name: 'previous', kind: 'folder', value: '/workspace/previous' }]
const succeededEvents = [
  { seq: 1, type: 'run_started' },
  { seq: 2, type: 'node_started', nodeId: 'execution', attempt: 1 },
  { seq: 3, type: 'node_output', nodeId: 'execution', status: 'done' },
  { seq: 4, type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: 'pass', reason: 'The reviewed implementation passes all checks.' },
  { seq: 5, type: 'node_started', nodeId: 'peer', attempt: 1 },
  { seq: 6, type: 'node_output', nodeId: 'peer', status: 'done' },
  { seq: 7, type: 'run_succeeded' },
]
// The same run ended through a pick (ADR-0022): a spent best card carried
// Execution's failed attempt on as the gate's pass, and the run ends picked.
const pickedEvents = [...succeededEvents.slice(0, 6), { seq: 7, type: 'run_picked' }]
/** The pick the picked run's outputs came through, as the daemon projects it on each. */
export const pickedExecution = { nodeId: 'execution', attempt: 1, gateId: 'gate', seq: 4, decidedBy: 'archon',
  verdict: { seq: 3, verdict: 'fail', reason: 'Retries are untested', findings: ['No test drives the retry path'] } }
const evidenceText = (text: string) => ({ text, bytes: text.length })
// Where a succeeded run snapshots a file or folder output (.archon/artifacts/<run>/<node>/<attempt>/...).
export const runArtifacts = '/srv/state/.archon/artifacts/run_browser'
// Media made by a run and cited by its judge. Paths deliberately include a
// space: real output folders need not be shell words.
export const mediaAttemptFixture = {
  attempt: 1, status: 'done', startedSeq: 2,
  outputs: [
    { portId: 'video', label: 'Turntable', kind: 'file', fileKind: 'video', artifact: 'execution/1/turntable.mp4', text: { text: '', bytes: 0 } },
    { portId: 'folder', label: 'Output folder', kind: 'folder', artifact: 'execution/1', path: `${runArtifacts}/execution/1`, text: { text: '', bytes: 0 } },
  ],
  verdicts: [{ gateId: 'gate', gateTitle: 'Review gate', evaluationSeq: 4, verdict: 'pass', reason: evidenceText('Watched the turntable.'),
    evidence: [{ kind: 'formation', text: evidenceText(`${runArtifacts}/execution/1/judge videos/review.webm`) }] }],
  files: [
    ...Array.from({ length: 180 }, (_, i) => ({ name: `execution/1/cache-${i}.txt`, path: `${runArtifacts}/execution/1/cache-${i}.txt`, kind: 'text', size: 1, modifiedAt: '' })),
    ...['folder-turntable.mp4', 'triangle.gltf'].map(name => ({ name: `execution/1/${name}`, path: `${runArtifacts}/execution/1/${name}`, kind: name.endsWith('.mp4') ? 'video' : 'model', size: 300, modifiedAt: '' })),
  ],
}
const OUTPUT_NAME = /^[a-z][a-z0-9_]{0,63}$/
/**
 * A succeeded run's outputs as the daemon projects them: each declared output
 * bound to a port, its value the port's text or the absolute path of its snapshot.
 */
function runOutputs(current: typeof board) {
  return (current.inputCards[0]?.outputs || []).filter(output => output.from).map(output => {
    const [nodeId, portId] = output.from!.split(':')
    const value = output.kind === 'file' ? `${runArtifacts}/${nodeId}/1/review.md`
      : output.kind === 'folder' ? `${runArtifacts}/${nodeId}/1/site` : 'Implemented the reviewed plan.\n\nAll tests pass.'
    return { name: output.name, kind: output.kind, value, nodeId, portId, attempt: 1 }
  })
}
// The snapshot files the file routes read for those outputs, by file name.
const snapshotText = (name: string): string | undefined => ({ 'review.md': reviewMarkdown, 'index.html': '<h1>Site</h1>' } as Record<string, string>)[name]
export const reviewMarkdown = '# Peer review\n\nVerdict: **revise**. The design and the implementation disagree on retries.\n\n- Keep the retry budget\n- Log the second failure'
const succeededEvidence: Record<string, unknown> = {
  '/api/runs/run_browser/evidence/nodes/gate': { evidence: { runId: 'run_browser', nodeId: 'gate', kind: 'gate',
    definition: { title: 'Review gate', outgoing: [] }, evaluations: [
      { seq: 2, kinds: ['human'], criterion: evidenceText('Review'), kindResults: [], verdict: { seq: 3, verdict: 'fail', reason: evidenceText('Earlier checks failed.'), evidence: [] } },
      { seq: 3, kinds: ['human'], criterion: evidenceText('Review'), kindResults: [], verdict: { seq: 4, verdict: 'pass', reason: evidenceText('The reviewed implementation passes all checks.'), evidence: [] } },
    ] } },
  '/api/runs/run_browser/evidence/nodes/execution': { evidence: { runId: 'run_browser', nodeId: 'execution', kind: 'formation',
    definition: { title: 'Execution', outputs: [{ id: 'out', label: 'Result' }], outgoing: [{ id: 'review', from: 'execution:out', to: 'gate:in' }] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 3, text: evidenceText('Implemented the reviewed plan.'), ports: [{ portId: 'out', text: evidenceText('Implemented the reviewed plan.\n\nAll tests pass.') }] } }] } },
  '/api/runs/run_browser/evidence/nodes/peer': { evidence: { runId: 'run_browser', nodeId: 'peer', kind: 'formation',
    definition: { title: 'Peer review', outputs: [{ id: 'out', label: 'Result' }], outgoing: [] },
    attempts: [{ attempt: 1, inputs: [], dispatches: [],
    output: { seq: 6, text: evidenceText(''), ports: [{ portId: 'out', text: evidenceText(reviewMarkdown), ref: { artifact: 'review.md' } }] } }] } },
  '/api/runs/run_browser/evidence/artifacts': { artifacts: [{ name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z' }, { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z' }], truncated: false, omitted: 0 },
  '/api/runs/run_browser/evidence/artifacts/review.md': { artifact: { name: 'review.md', size: reviewMarkdown.length, modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', text: evidenceText(reviewMarkdown) } },
  '/api/runs/run_browser/evidence/artifacts/logs/worker.log': { artifact: { name: 'logs/worker.log', size: 40, modifiedAt: '2026-09-16T00:00:00Z', kind: 'text', text: evidenceText('worker started\nworker finished') } },
}

// The run is blocked before Execution's third run: its Limit card Cap allows 2
// rounds, both used (archon-o7p.8). Its gate judged both attempts, so it
// resumes with one more round granted or by picking the better (archon-gihe).
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

type Role = { id: string; displayName: string; kind: string; summary?: string }

export async function cockpitFixture(page: Page, options: { noLayout?: boolean; declaredInputs?: boolean; far?: boolean; run?: boolean; blockedAtJudge?: boolean; blockedAtLimit?: boolean; blockedAtTime?: boolean; succeeded?: boolean; picked?: boolean; themeFailure?: boolean; waitingHuman?: boolean; secondGate?: boolean; join?: boolean; extraAgents?: number; vanillaWorker?: boolean; emptyWorker?: boolean; roles?: Role[]; characters?: Array<Record<string, unknown>>; workers?: Array<Record<string, unknown>>; newRow?: boolean } = {}) {
  const currentBoard = structuredClone(board)
  // A CLI-authored mission has declarations but no saved canvas layout.
  if (options.declaredInputs) {
    currentBoard.inputCards[0].inputs = [
      { name: 'subject', kind: 'text', required: true },
      { name: 'facts', kind: 'file', required: true },
      { name: 'previous', kind: 'folder' },
    ]
    currentBoard.inputCards[0].outputs = [{ name: 'summary', kind: 'text', from: 'execution:out', required: true }]
  }
  if (options.vanillaWorker) {
    // A slot with no role is a vanilla agent; this one runs a model the catalog names.
    currentBoard.formations[0].slots[1] = { id: 'worker', label: 'Worker 1', harness: 'claude-code', model: 'opus', effort: 'low', controller: false }
  }
  if (options.emptyWorker) currentBoard.formations[0].slots[1] = { id: 'worker', label: 'Worker 1', controller: false }
  if (options.workers) currentBoard.formations[0].slots = [currentBoard.formations[0].slots[0], ...options.workers]
  if (options.noLayout) {
    // CLI/TOML authoring supplies inputs and graph edges, but no canvas positions.
    currentBoard.inputCards[0].inputs = [
      { name: 'subject', kind: 'text', required: true, description: 'Character to build' },
      { name: 'facts', kind: 'file', required: true, description: 'Authored identity facts' },
      { name: 'reference', kind: 'file', required: true, description: 'Approved plate' },
      { name: 'body', kind: 'folder', required: true, description: 'Cleaned body' },
      { name: 'lane', kind: 'text', required: true, description: 'Owning work' },
    ]
    currentBoard.gates[0].title = 'MetaHuman is the character and moves'
    currentBoard.limits = [{ id: 'lim_cap', title: 'Execution rounds', target: 'execution', rounds: 3 }]
    currentBoard.ends = [{ id: 'end_done', title: 'Done', outcome: 'done' }]
    currentBoard.connections.push({ id: 'retry', from: 'gate:fail', to: 'execution:in' },
      { id: 'finish', from: 'peer:out', to: 'end_done:in' })
  }
  const patches: Array<Record<string, unknown>> = []
  let characters = structuredClone(options.characters || [])
  const characterWrites: Array<{ method: string; id: string; body: Record<string, unknown> }> = []
  // A time block is a limit block too, with a time card.
  const timeBlock = Boolean(options.blockedAtTime)
  if (timeBlock) options = { ...options, blockedAtLimit: true }
  if (options.blockedAtLimit) {
    // Execution's send-back route leads back to it, and its card allows 2 rounds, or 30 min warned at 5 min left.
    currentBoard.limits = [timeBlock ? { id: 'lim_cap', title: 'Cap', target: 'execution', seconds: 1800, warnSeconds: 300 } : { id: 'lim_cap', title: 'Cap', target: 'execution', rounds: 2 }]
    currentBoard.connections = [...currentBoard.connections, { id: 'gate-fail', from: 'gate:fail', to: 'execution:in' }]
  }
  // The run resumes once the grant, or the pick of the best attempt, is recorded.
  let granted = false
  let picked = false
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
  // A row of three formations just made with New formation: solo, one empty Agent slot each, under the graph.
  const newRow = ['new_1', 'new_2', 'new_3']
  if (options.newRow) {
    currentBoard.formations = [...currentBoard.formations, ...newRow.map(id => ({ id, type: 'solo', title: 'New formation', ...ports, slots: [{ id: 'agent', label: 'Agent', controller: false }] }))]
  }
  const boardState = () => currentBoard
  let nodes = options.noLayout || options.declaredInputs ? [] : positions.map(p => ({ ...p, x: p.x + (options.far ? 1800 : 0) }))
  if (options.newRow) nodes = [...nodes, ...newRow.map((id, index) => ({ id, x: 448 + index * 336, y: 896 }))]
  if (options.join) nodes = [{ id: 'a', x: 100, y: 80 }, { id: 'b', x: 100, y: 350 }, { id: 'c', x: 100, y: 620 }, { id: 'sink', x: 650, y: 350 }]
  if (options.waitingHuman) nodes = [...nodes, { id: 'end_done', x: 504, y: 672 }, { id: 'end_rejected', x: 504, y: 784 }]
  if (options.secondGate) nodes = [...nodes, { id: 'second', x: 112, y: 920 }]
  if (options.blockedAtLimit) nodes = [...nodes, { id: 'lim_cap', x: 448, y: 476 }]
  const endsOf = () => (currentBoard.ends ||= [])
  const limitsOf = () => (currentBoard.limits ||= [])
  // Mirrors the store (checkLimitWrite): a target must be a step or the Input card, and no knob negative.
  type Knobs = { rounds?: number; seconds?: number; warnSeconds?: number; tokens?: number; whenSpent?: string }
  const limitRefusal = (target: string | undefined, knobs: Knobs) => {
    if (target && ![...currentBoard.inputCards, ...currentBoard.formations].some(node => node.id === target)) {
      return `"${target}" is not a step or the Input card; a Limit card covers one of them`
    }
    if (knobs.rounds !== undefined && knobs.rounds < 0) return 'rounds must be a positive whole number'
    if (knobs.seconds !== undefined && knobs.seconds < 0) return 'time must be a positive whole number of seconds'
    if (knobs.warnSeconds !== undefined && knobs.warnSeconds < 0) return 'the warning must be a positive whole number of seconds'
    if (knobs.tokens !== undefined && knobs.tokens < 0) return 'tokens must be a positive whole number'
    if (knobs.whenSpent !== undefined && !['', 'stop', 'best', 'fail'].includes(knobs.whenSpent)) return `invalid_limit: whenSpent "${knobs.whenSpent}" must be stop, best or fail`
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
        patches.push(body)
        if (body.assignSlot) {
          const { formationId, slotId } = body.assignSlot
          const formation = currentBoard.formations.find(item => item.id === formationId)
          const index = formation?.slots.findIndex(slot => slot.id === slotId) ?? -1
          if (!formation || index < 0) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          const assigned = assignedSettings(slotId, body.assignSlot)
          if ('refused' in assigned) return route.fulfill({ status: 422, json: { success: false, error: { code: 'INVALID_SLOT_SETTINGS', message: assigned.refused } } })
          const { id, label, controller } = formation.slots[index] as { id: string; label: string; controller: boolean }
          formation.slots[index] = { id, label, controller, ...assigned.settings }
          currentBoard.rev++
          currentBoard.etag = `board-${currentBoard.rev}`
          const harness = harnesses.find(entry => entry.id === assigned.settings.harness)
          const model = assigned.settings.model
          const warnings = model && harness && !harness.models.some(entry => entry.id === model)
            ? [`slot "${label}" (${id}) model "${model}" is not in the ${harness.id} catalog; the harness decides`] : undefined
          return respond({ mission: boardState(), ...(warnings ? { warnings } : {}) })
        }
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
          const { title, target = '', rounds = 0, seconds = 0, warnSeconds = 0, tokens = 0, whenSpent = '', x, y } = body.createLimit
          const refused = refuseLimit(target, { rounds, seconds, warnSeconds, tokens, whenSpent })
          if (refused) return refused
          const limit: LimitNode = { id: `lim_${currentBoard.rev}`, title: title || 'Limit', target, ...(rounds ? { rounds } : {}), ...(seconds ? { seconds } : {}), ...(warnSeconds ? { warnSeconds } : {}), ...(tokens ? { tokens } : {}), ...(['best', 'fail'].includes(whenSpent) ? { whenSpent: whenSpent as 'best' | 'fail' } : {}) }
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
          // whenSpent stop is stored as absent (archon-gihe).
          if (knobs.whenSpent === 'best' || knobs.whenSpent === 'fail') limit.whenSpent = knobs.whenSpent
          else if (knobs.whenSpent !== undefined) delete limit.whenSpent
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
          // An output port may carry a file or folder; text is the default and is not stored.
          const { kind } = body.addPort
          if (kind !== undefined && (direction === 'input' || !['text', 'file', 'folder'].includes(kind))) {
            return route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_PORT', message: `port kind "${kind}" must be text, file or folder, on an output port` } } })
          }
          const port: Port = { id: `port_${currentBoard.rev}`, label: label || (direction === 'input' ? 'Input' : 'Output'), ...(kind && kind !== 'text' ? { kind } : {}) }
          if (direction === 'input') formation.inputs = [...formation.inputs, port]
          else formation.outputs = [...formation.outputs, port]
        } else if (body.updatePort) {
          // Mirrors the store (UpdateFormationPort): only fields present change; text clears an output's kind.
          const { formationId, portId, label, kind } = body.updatePort
          const formation = currentBoard.formations.find(item => item.id === formationId)
          const input = formation?.inputs.find(port => port.id === portId)
          const port = input || formation?.outputs.find(port => port.id === portId)
          if (!port) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          const refuse = (message: string) => route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_PORT', message } } })
          if (label !== undefined && !String(label).trim()) return refuse('a port label cannot be blank')
          if (kind !== undefined && (input || !['text', 'file', 'folder'].includes(kind))) return refuse(`port kind "${kind}" must be text, file or folder, on an output port`)
          if (label !== undefined) port.label = label
          if (kind === 'text') delete port.kind
          else if (kind !== undefined) port.kind = kind
        } else if (body.updateInputCard) {
          // Mirrors the store (UpdateInputCard): only fields present change; outputs replace the declared ones.
          const { id, ...fields } = body.updateInputCard
          const card = currentBoard.inputCards.find(item => item.id === id)
          if (!card) return route.fulfill({ status: 404, json: { success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } } })
          const outputs: MissionOutput[] | undefined = fields.outputs
          const names = new Set<string>()
          for (const output of outputs || []) {
            const bad = !OUTPUT_NAME.test(output.name) ? `output name "${output.name}" must start with a lowercase letter`
              : names.has(output.name) ? `two outputs are named ${output.name}`
                : !['text', 'file', 'folder'].includes(output.kind) ? `output kind "${output.kind}" must be text, file or folder`
                  : output.from && !/^[^:]+:[^:]+$/.test(output.from) ? `output from "${output.from}" must be <step-id>:<port-id>` : ''
            if (bad) return route.fulfill({ status: 400, json: { success: false, error: { code: 'INVALID_MISSION_OUTPUT', message: bad } } })
            names.add(output.name)
          }
          Object.assign(card, fields)
          if (outputs && !outputs.length) delete card.outputs
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
    if (path === '/api/characters' && method === 'GET') return respond({ characters })
    if (path.startsWith('/api/characters')) {
      const id = decodeURIComponent(path.slice('/api/characters/'.length))
      const body = method === 'DELETE' ? {} : route.request().postDataJSON()
      characterWrites.push({ method, id, body })
      if (method === 'DELETE') characters = characters.filter(character => character.id !== id)
      else if (method === 'PUT') characters = characters.map(character => character.id === id ? { ...body, id } : character)
      else characters = [...characters, { ...body, id: String(body.name).toLowerCase().replace(/[^a-z0-9]+/g, '-') }]
      return respond(method === 'DELETE' ? { deleted: id } : characters.find(character => character.id === id) || characters.at(-1))
    }
    if (path === '/api/agents') return respond(rosterAnswer([
      { id: 'claude', displayName: 'Claude controller', summary: 'Directs the workers.', assignable: true, liveness: 'live', tags: [], kind: 'controller' },
      { id: 'codex', displayName: 'Codex builder', summary: 'Builds the change.', assignable: true, liveness: 'live', tags: [], kind: 'builder' },
      ...(options.roles || []).map(role => ({ ...role, summary: role.summary || '', assignable: true, liveness: 'offline', tags: [] })),
      // A large roster, as on a real host, makes long staffing menus.
      ...Array.from({ length: options.extraAgents || 0 }, (_, index) => ({ id: `agent-${index + 1}`, displayName: `Roster agent ${index + 1}`, assignable: true, liveness: 'live', tags: [], kind: 'builder' })),
    ]))
    if (path === '/api/agents/codex') return respond({ id: 'codex', displayName: 'Codex builder', kind: 'builder', summary: 'Builds the change.', tags: [], etag: 'codex-card' })
    if (path === '/api/runs/run_browser' && options.waitingHuman) return respond({ runId: 'run_browser', status: 'waiting_human', final: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: options.secondGate ? 4 : 3, cwd: runCwd,
      waitingGates: [{ gateId: 'loose', requestedSeq: 3 }, ...(options.secondGate ? [{ gateId: 'second', requestedSeq: 4 }] : [])] })
    if (path === '/api/runs/run_browser' && options.succeeded) {
      // A picked run (succeeded with picked) carried Execution's failed first
      // attempt on through its gate, so every output came through that pick.
      const outputs = runOutputs(currentBoard).map(output => options.picked ? { ...output, picks: [pickedExecution] } : output)
      return respond({ runId: 'run_browser', status: options.picked ? 'picked' : 'succeeded', final: true, missionSlug: 'browser', inputCardId: 'mission', eventCount: 7, cwd: runCwd, ...(options.declaredInputs ? { inputs: flowInputs } : {}), ...(outputs.length ? { outputs } : {}) })
    }
    // The file routes read a succeeded run's output snapshots by absolute path.
    if (options.succeeded && path === '/api/files/directory') {
      const folder = url.searchParams.get('path') || ''
      if (!folder.startsWith(runArtifacts) && folder !== '/workspace/previous') return route.fulfill({ status: 404, json: { success: false, error: { message: 'directory not found' } } })
      return respond({ items: [...(folder.endsWith('/assets') ? [] : [{ name: 'assets', path: `${folder}/assets`, isDir: true }]), { name: 'index.html', path: `${folder}/index.html`, isDir: false }] })
    }
    if (options.succeeded && path === '/api/files/preview') {
      const file = url.searchParams.get('path') || ''
      const text = file === '/workspace/identity.md' ? '# Identity facts\n\nKeep the character consistent.' : file.startsWith(runArtifacts) ? snapshotText(file.split('/').pop() || '') : undefined
      if (text === undefined) return route.fulfill({ status: 404, json: { success: false, error: { message: 'file not found' } } })
      return respond({ file: { path: file, name: file.split('/').pop(), size: text.length, modifiedAt: '', kind: file.endsWith('.md') ? 'markdown' : 'text', text: evidenceText(text) } })
    }
    if (options.succeeded && path in succeededEvidence) return respond(succeededEvidence[path])
    if (options.blockedAtLimit && path === '/api/runs/run_browser/resume' && method === 'POST') {
      // Mirrors the coordinator: a spent limit whose step the gate judged
      // resumes with a grant or a pick of the best attempt (archon-gihe).
      const body = route.request().postDataJSON()
      if (body.grant && body.pickBest) return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'give one more allowance with --grant or pick the best attempt with --pick-best, not both' } } })
      if (!body.grant && !body.pickBest) return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: timeBlock
        ? 'this run stopped at a spent limit: resume it with --grant to give one more round or the card\'s time or tokens again'
        : 'this run stopped at a spent limit: resume it with --grant to give one more round or the card\'s time or tokens again, or with --pick-best to take the best attempt its gate judged and go on' } } })
      // The time block's step was never judged, so it has nothing to pick.
      if (body.pickBest && timeBlock) return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'nothing to pick: this run did not stop at a spent limit of a step whose gate judged an attempt' } } })
      if (body.pickBest) picked = true
      else granted = true
      return respond({ runId: 'run_browser', status: 'running', final: false, resumeAllowed: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length + 3, cwd: runCwd })
    }
    if (path === '/api/runs/run_browser' && options.blockedAtLimit) return respond(granted || picked
      ? { runId: 'run_browser', status: 'running', final: false, resumeAllowed: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length + (picked ? 3 : 2), cwd: runCwd }
      : { runId: 'run_browser', status: 'blocked', final: false, resumeAllowed: true, resumePolicy: timeBlock ? 'grant' : 'grant_or_pick', missionSlug: 'browser', inputCardId: 'mission', eventCount: blockEvents.length, cwd: runCwd })
    // A pick ends in the gate's pass with the picked attempt, as an ordinary pass routes.
    const pickEvents = [{ seq: blockEvents.length + 2, type: 'pick_started', nodeId: 'gate', gateId: 'gate' },
      { seq: blockEvents.length + 3, type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: 'pass' }]
    if (path.endsWith('/events') && options.blockedAtLimit) return respond({ events: granted
      ? [...blockEvents, { seq: blockEvents.length + 1, type: 'run_resumed' }, { seq: blockEvents.length + 2, type: 'node_started', nodeId: 'execution', attempt: timeBlock ? 2 : 3 }]
      : picked ? [...blockEvents, { seq: blockEvents.length + 1, type: 'run_resumed' }, ...pickEvents]
        : blockEvents })
    if (options.blockedAtLimit && path === '/api/runs/run_browser/evidence/problems') return respond({ problems: [
      { seq: blockEvents.length - 1, type: 'error', code: 'limit_reached', nodeIds: ['execution'], reason: { text: blockReason, bytes: blockReason.length }, limit: blockUse },
      { seq: blockEvents.length, type: 'run_blocked', code: 'limit_reached', nodeIds: ['execution'], reason: { text: blockReason, bytes: blockReason.length }, resumeAllowed: true, limit: blockUse, ...(granted || picked ? { resumedSeq: blockEvents.length + 1 } : {}) },
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
    if (path.endsWith('/events') && options.succeeded) return respond({ events: options.picked ? pickedEvents : succeededEvents })
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
  return { writes, patches, characterWrites, board: boardState, seatsFetches: () => seatsFetches, themeFetches: () => themeFetches }
}
