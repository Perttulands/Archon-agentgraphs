import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FormationsCockpit from './FormationsCockpit'
import type { AgentProjection } from './formationsTypes'

/* First direct cockpit coverage: pins the reference-parity behaviors that the
   prototype (03-formations.js) defines — judge wires render, the right-click
   surface exists everywhere, menus dismiss, and gestures issue real board ops. */

const judgeFormation = {
  id: 'fmn_judge',
  type: 'solo',
  title: 'Judge',
  inputs: [{ id: 'port_judge_in', label: 'Input' }],
  outputs: [{ id: 'port_judge_out', label: 'Output' }],
  slots: [{ id: 'slot_judge', label: 'Judge' }],
  verification: undefined,
}

const formation = {
  id: 'fmn_frame',
  type: 'orchestrated',
  title: 'Frame',
  inputs: [{ id: 'port_frame_in', label: 'Input' }],
  outputs: [{ id: 'port_frame_out', label: 'Output' }],
  slots: [
    { id: 'slot_lead', label: 'Lead', controller: true, agentId: 'mason', harness: 'codex' },
    { id: 'slot_worker', label: 'Worker', controller: false },
  ],
  verification: { id: 'ver_frame', kinds: ['code'], criterion: 'Tests pass', onFail: 'block' },
}

type TestGate = { id: string; title: string; kinds: string[]; criterion: string; check?: string; checkVersion?: string; checkValue?: string }
const gate: TestGate = { id: 'gate_review', title: 'Review', kinds: ['code'], criterion: 'Review the frame' }
const mission = { id: 'mis_showcase', title: 'Showcase', goal: 'Build the page', beadId: 'home-7kc4.5' }
const tool = {
  id: 'tool_normalize',
  title: 'Normalize report',
  profileId: 'json.normalize',
  profileVersion: '1',
  params: { mode: 'strict' },
  inputs: [{
    id: 'port_tool_input',
    name: 'input',
    label: 'Report',
    direction: 'input' as const,
    kind: 'work' as const,
    acceptedMediaTypes: ['application/json'],
    required: true,
    role: 'data' as const,
  }],
  outputs: [{
    id: 'port_tool_output',
    name: 'output',
    label: 'Normalized report',
    direction: 'output' as const,
    kind: 'work' as const,
    acceptedMediaTypes: ['application/json'],
  }],
}
const upstreamTool = {
  ...tool,
  id: 'tool_source',
  title: 'Source JSON',
  inputs: tool.inputs.map(port => ({ ...port, id: 'port_source_input' })),
  outputs: tool.outputs.map(port => ({ ...port, id: 'port_source_output' })),
}

function makeBoard() {
  return {
    schema: 1,
    id: 'brd_test',
    slug: 'test-board',
    title: 'Test board',
    rev: 7,
    etag: 'board-etag',
    missions: [mission],
    formations: [formation, judgeFormation],
    gates: [gate],
    tools: [] as typeof tool[],
    connections: [
      { id: 'edge_mission_frame', from: 'mis_showcase:out', to: 'fmn_frame:port_frame_in' },
      { id: 'edge_frame_gate', from: 'fmn_frame:port_frame_out', to: 'gate_review:in' },
      { id: 'edge_judge_send', from: 'gate_review:judge', to: 'fmn_judge:port_judge_in' },
      { id: 'edge_judge_return', from: 'fmn_judge:port_judge_out', to: 'gate_review:judge' },
    ],
  }
}

function makeToolBoard() {
  const base = makeBoard()
  return {
    ...base,
    schema: 2,
    tools: [upstreamTool, tool],
    connections: [
      ...base.connections.filter(connection => connection.id !== 'edge_frame_gate'),
      { id: 'edge_tool_chain', from: 'tool_source:port_source_output', to: 'tool_normalize:port_tool_input' },
      { id: 'edge_tool_gate', from: 'tool_normalize:port_tool_output', to: 'gate_review:in' },
    ],
  }
}

const layout = {
  schema: 1,
  boardId: 'brd_test',
  boardRev: 7,
  etag: 'layout-etag',
  nodes: [
    { id: 'mis_showcase', x: 100, y: 100 },
    { id: 'fmn_frame', x: 420, y: 100 },
    { id: 'fmn_judge', x: 700, y: 420 },
    { id: 'gate_review', x: 860, y: 100 },
  ],
  edges: [],
}

const agents: AgentProjection[] = [
  { id: 'mason', displayName: 'Mason', harnessDefault: 'codex', liveness: 'live', assignable: true, unbound: false },
  { id: 'hazel', displayName: 'Hazel', harnessDefault: 'claude', liveness: 'live', assignable: true, unbound: false },
  { id: 'scratch', displayName: 'scratch', liveness: 'live', assignable: false, unbound: true },
]

type RecordedPatch = { url: string; body: Record<string, unknown> }
type RecordedMutation = { method: string; url: string }
type TestBoard = ReturnType<typeof makeBoard>
type TestRunEvent = { runId: string; seq: number; type: string; nodeId?: string; gateId?: string; attempt?: number; data?: Record<string, unknown>; slotId?: string; status?: string; verdict?: string; sessionName?: string; outcome?: string }
type TestEscalation = { runId: string; seq: number; nodeId?: string; gateId?: string; severity: string; reason: string; source: string; trigger: string; blocks: boolean }
type TestRunStatus = { status?: string; final?: boolean; resumeAllowed?: boolean }
type TestFinding = { code: string; nodeId: string; message: string }
type TestNoteEntry = { id: string; author: string; createdAt: string; editedAt?: string; text: string }
const noteEntry = (id: string, author: string, text: string): TestNoteEntry => ({ id, author, createdAt: '2026-09-16T12:00:00Z', text })
let recordedMutations: RecordedMutation[] = []

function installFetchMock(options: {
  emptyBoards?: boolean
  freshCreateLayout?: boolean
  missionCreateFailure?: boolean
  removalFailure?: boolean
  removalGate?: Promise<void>
  boards?: TestBoard[]
  sameBoardRefreshes?: TestBoard[]
  runEvents?: TestRunEvent[]
  escalations?: TestEscalation[]
  runStatus?: TestRunStatus
  boardNotes?: { board?: TestNoteEntry[]; elements?: Array<{ nodeId: string; entries: TestNoteEntry[] }> }
  notePatchConflict?: boolean
  agents?: typeof agents
  agentDetailGate?: Promise<void>
  validation?: { errors: TestFinding[]; warnings: TestFinding[] }
  runStartFindings?: TestFinding[]
} = {}) {
  const patches: RecordedPatch[] = []
  recordedMutations = []
  let availableBoards = options.emptyBoards ? [] : (options.boards?.length ? options.boards : [makeBoard()])
  let board = availableBoards[0] || makeBoard()
  let availableAgents = options.agents || agents
  let currentLayout = layout
  let boardNotes = {
    schema: 2,
    boardId: board.id,
    rev: options.boardNotes ? 1 : 0,
    updatedAt: '2026-08-18T13:00:00Z',
    updatedBy: 'human:test',
    board: options.boardNotes?.board || [] as TestNoteEntry[],
    elements: options.boardNotes?.elements || [] as Array<{ nodeId: string; entries: TestNoteEntry[] }>,
    etag: options.boardNotes ? 'notes-etag' : '*',
  }
  let nextNoteID = 1
  ;(globalThis as Record<string, unknown>).fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = (init?.method || 'GET').toUpperCase()
    if (method !== 'GET') recordedMutations.push({ method, url })
    const respond = (data: unknown, etag = '') => Promise.resolve({
      ok: true,
      headers: { get: (name: string) => (name.toLowerCase() === 'etag' ? etag || null : null) },
      json: () => Promise.resolve({ success: true, data }),
      text: () => Promise.resolve(''),
    })
    const reject = (message: string) => Promise.resolve({
      ok: false,
      status: 400,
      headers: { get: () => null },
      json: () => Promise.resolve({ success: false, error: { code: 'BAD_REQUEST', message } }),
      text: () => Promise.resolve(message),
    })
    const conflict = (message: string) => Promise.resolve({
      ok: false,
      status: 409,
      headers: { get: () => null },
      json: () => Promise.resolve({ success: false, error: { code: 'CONFLICT', message } }),
      text: () => Promise.resolve(message),
    })
    if (method === 'POST' && url === '/api/formations/boards') {
      const body = JSON.parse(String(init?.body)) as { title: string }
      const created = {
        ...makeBoard(),
        id: 'brd_created',
        slug: 'release-plan',
        title: body.title,
        rev: 1,
        etag: 'created-board-etag',
        missions: [],
        formations: [],
        gates: [],
        connections: [],
      }
      availableBoards = [...availableBoards, created]
      board = created
      currentLayout = { schema: 1, boardId: created.id, boardRev: created.rev, etag: '*', nodes: [], edges: [] }
      boardNotes = { ...boardNotes, boardId: created.id, rev: 0, board: [], elements: [], etag: '*' }
      return respond({ board: created }, created.etag)
    }
    if (method === 'DELETE' && url.includes('/api/formations/boards/')) {
      availableBoards = availableBoards.filter(item => item.slug !== board.slug)
      return respond({ deletion: { id: board.id, slug: board.slug, title: board.title, archiveId: 'archive_test' } })
    }
    if (method === 'GET' && /^\/api\/formations\/boards\/[^/]+\/notes$/.test(url)) {
      return respond({ notes: boardNotes }, boardNotes.etag)
    }
    if (method === 'PATCH' && /^\/api\/formations\/boards\/[^/]+\/notes$/.test(url)) {
      if (options.notePatchConflict) return conflict('Shared notes changed; reload and retry')
      const body = JSON.parse(String(init?.body)) as { target: string; action: string; entryId?: string; text?: string; author: string }
      const thread = body.target === 'board' ? boardNotes.board : boardNotes.elements.find(note => note.nodeId === body.target)?.entries || []
      const nextThread = body.action === 'append'
        ? [...thread, noteEntry(`nte_new_${nextNoteID++}`, body.author, body.text || '')]
        : body.action === 'edit'
          ? thread.map(entry => entry.id === body.entryId ? { ...entry, text: body.text || '', editedAt: '2026-09-16T13:00:00Z' } : entry)
          : thread.filter(entry => entry.id !== body.entryId)
      boardNotes = {
        ...boardNotes,
        rev: boardNotes.rev + 1,
        updatedBy: body.author,
        etag: `notes-etag-${boardNotes.rev + 1}`,
        board: body.target === 'board' ? nextThread : boardNotes.board,
        elements: body.target === 'board'
          ? boardNotes.elements
          : [...boardNotes.elements.filter(note => note.nodeId !== body.target), ...(nextThread.length ? [{ nodeId: body.target, entries: nextThread }] : [])],
      }
      return respond({ notes: boardNotes }, boardNotes.etag)
    }
    const agentMatch = url.match(/^\/api\/agents\/([^/]+)$/)
    if (agentMatch) {
      const id = decodeURIComponent(agentMatch[1])
      const agent = availableAgents.find(candidate => candidate.id === id)
      if (!agent) return reject('Agent not found')
      const harness = agent.harnessDefault || 'claude-code'
      const defaultVariant = {
        id: harness,
        model: `${agent.id}-model`,
        effort: 'medium',
        sessionStem: agent.id,
        launch: harness === 'openai-codex'
          ? 'codex --yolo -c check_for_update_on_startup=false'
          : 'claude --dangerously-skip-permissions --effort="max"',
      }
      if (method === 'PATCH') {
        const payload = JSON.parse(String(init?.body || '{}')) as Record<string, unknown>
        const updated = {
          ...agent,
          displayName: String(payload.displayName || agent.displayName || agent.id),
          kind: String(payload.kind || agent.kind || 'specialist'),
          tags: Array.isArray(payload.capabilities) ? payload.capabilities.map(String) : (agent.tags || []),
          customized: true,
        }
        availableAgents = availableAgents.map(candidate => candidate.id === id ? updated : candidate)
        return respond({
          ...updated,
          summary: String(payload.summary || ''),
          harnessVariants: [{
            ...defaultVariant,
            sessionStem: String(payload.sessionStem || defaultVariant.sessionStem),
            launch: String(payload.launch || defaultVariant.launch),
          }],
          etag: `${id}-etag-2`,
        }, `${id}-etag-2`)
      }
      const respondWithAgent = () => respond({
          ...agent,
          kind: agent.kind || 'specialist',
          summary: `${agent.displayName || agent.id} summary`,
          tags: agent.tags || [],
          harnessVariants: [defaultVariant],
          etag: `${id}-etag`,
        }, `${id}-etag`)
      return options.agentDetailGate ? options.agentDetailGate.then(respondWithAgent) : respondWithAgent()
    }
    if (init?.method === 'PATCH') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      patches.push({ url, body })
      if (!url.endsWith('/layout') && typeof body.title === 'string') {
        board = { ...board, title: body.title, rev: board.rev + 1, etag: 'board-etag-2' }
        availableBoards = availableBoards.map(item => item.slug === board.slug ? board : item)
        return respond({ board }, board.etag)
      }
      if (!url.endsWith('/layout') && body.createMission) {
        if (options.missionCreateFailure) return reject('Mission create failed')
        const requested = body.createMission as { title: string; goal: string; beadId: string; x: number; y: number }
        const created = {
          id: 'mis_created',
          title: requested.title,
          goal: requested.goal,
          beadId: requested.beadId,
        }
        board = { ...board, rev: board.rev + 1, missions: [...board.missions, created] }
        currentLayout = {
          ...currentLayout,
          boardRev: board.rev,
          etag: 'layout-mission-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: requested.x, y: requested.y }],
        }
        return respond({ board, layout: currentLayout }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.setFormationType) {
        type Slot = { id: string; label: string; controller?: boolean; agentId?: string; harness?: string }
        const { id, type, keepSlotId, slots } = body.setFormationType as { id: string; type: string; keepSlotId?: string; slots?: Slot[] }
        board = {
          ...board,
          rev: board.rev + 1,
          formations: board.formations.map(item => {
            if (item.id !== id) return item
            const current = item.slots as Slot[]
            let next = slots
            if (!next && type === 'solo') next = [{ ...(current.find(slot => slot.id === keepSlotId) || current.find(slot => slot.agentId) || current[0]), controller: false }]
            if (!next && type === 'peer') next = current.map(slot => ({ ...slot, controller: false }))
            if (!next) next = current.map((slot, index) => ({ ...slot, controller: index === 0 }))
            return { ...item, type, slots: next }
          }) as TestBoard['formations'],
        }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.setBrief) {
        const { formationId, ...brief } = body.setBrief as { formationId: string; goal: string; beadId: string; files: string[]; links: string[] }
        board = { ...board, rev: board.rev + 1, formations: board.formations.map(item => item.id === formationId ? { ...item, brief } : item) as TestBoard['formations'] }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.assignSlot) {
        const { formationId, slotId, agentId, harness } = body.assignSlot as { formationId: string; slotId: string; agentId: string; harness: string }
        board = {
          ...board,
          rev: board.rev + 1,
          formations: board.formations.map(item => item.id !== formationId ? item : {
            ...item,
            slots: item.slots.map(slot => slot.id === slotId ? { ...slot, agentId: agentId || undefined, harness: harness || undefined } : slot),
          }) as TestBoard['formations'],
        }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.updateFormation) {
        const { id, title } = body.updateFormation as { id: string; title: string }
        board = { ...board, rev: board.rev + 1, formations: board.formations.map(item => item.id === id ? { ...item, title } : item) }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.updateMission) {
        const { id, ...fields } = body.updateMission as { id: string; title?: string; goal?: string; beadId?: string }
        board = { ...board, rev: board.rev + 1, missions: board.missions.map(item => item.id === id ? { ...item, ...fields } : item) }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.updateGate) {
        const requested = body.updateGate as Partial<TestGate> & { id: string }
        const { id, ...fields } = requested
        let dropsJudge = false
        const gates = board.gates.map(item => {
          if (item.id !== id) return item
          const next = { ...item, ...fields }
          dropsJudge = item.kinds.includes('formation') && !next.kinds.includes('formation')
          return next
        })
        const connections = dropsJudge
          ? board.connections.filter(connection => connection.from !== `${id}:judge` && connection.to !== `${id}:judge`)
          : board.connections
        board = { ...board, rev: board.rev + 1, gates, connections }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.detachGateJudge) {
        // Mirrors the store: drop formation, and a judge-only gate becomes human.
        const { gateId } = body.detachGateJudge as { gateId: string }
        const gates = board.gates.map(item => {
          if (item.id !== gateId) return item
          const kinds = item.kinds.filter(kind => kind !== 'formation')
          return { ...item, kinds: kinds.length ? kinds : ['human'] }
        })
        const connections = board.connections.filter(connection => connection.from !== `${gateId}:judge` && connection.to !== `${gateId}:judge`)
        board = { ...board, rev: board.rev + 1, gates, connections }
        return respond({ board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.createFormation) {
        const requested = body.createFormation as { type: string; title: string; x: number; y: number }
        const created = {
          id: 'fmn_created',
          type: requested.type,
          title: requested.title,
          inputs: [{ id: 'port_created_in', label: 'Input' }],
          outputs: [{ id: 'port_created_out', label: 'Output' }],
          slots: [{ id: 'slot_created', label: 'Agent' }],
          verification: undefined,
        }
        board = { ...board, rev: board.rev + 1, formations: [...board.formations, created] as TestBoard['formations'] }
        currentLayout = {
          ...currentLayout,
          boardRev: board.rev,
          etag: 'layout-formation-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: requested.x, y: requested.y }],
        }
        return respond({ board, layout: currentLayout }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.removeVerification && options.removalFailure) {
        return reject('Legacy verification migration failed')
      }
      if (!url.endsWith('/layout') && body.removeVerification && options.removalGate) {
        return options.removalGate.then(() => {
          board = { ...board, rev: board.rev + 1 }
          return respond({ board }, 'board-etag-2')
        })
      }
      if (options.freshCreateLayout && !url.endsWith('/layout') && body.createGate) {
        const requested = body.createGate as { title: string; kinds: string[]; criterion: string; check: string; checkVersion: string; checkValue: string }
        const created = { id: 'gate_created', title: requested.title, kinds: requested.kinds, criterion: requested.criterion, check: requested.check, checkVersion: requested.checkVersion, checkValue: requested.checkValue }
        board = { ...board, rev: board.rev + 1, gates: [...board.gates, created] }
        currentLayout = {
          ...currentLayout,
          boardRev: board.rev,
          etag: 'layout-created-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: 1344, y: 784 }],
        }
        return respond({ board, layout: currentLayout }, 'board-etag-2')
      }
      board = { ...board, rev: board.rev + 1 }
      if (url.endsWith('/layout')) return respond({ layout: currentLayout }, 'layout-etag-2')
      return respond({ board }, 'board-etag-2')
    }
    if (url === '/api/formations/runs' && init?.method === 'POST' && options.runStartFindings) {
      const findings = options.runStartFindings
      return Promise.resolve({
        ok: false,
        status: 422,
        headers: { get: () => null },
        json: () => Promise.resolve({ success: false, error: { code: 'RUN_ADMISSION_FAILED', message: `The run needs ${findings.length} fixes before it can start`, findings } }),
        text: () => Promise.resolve(''),
      })
    }
    if (url === '/api/formations/runs' && init?.method === 'POST') return respond({ runId: 'run_legacy' })
    if (/\/api\/formations\/runs\/[^/]+\/escalations$/.test(url)) return respond({ escalations: options.escalations || [] })
    if (/\/api\/formations\/runs\/[^/]+\/events$/.test(url)) return respond({ events: options.runEvents || [] })
    if (/\/api\/formations\/runs\/[^/]+$/.test(url)) {
      return respond({ status: {
        runId: 'run_legacy',
        status: options.runStatus?.status ?? 'succeeded',
        final: options.runStatus?.final ?? true,
        resumeAllowed: options.runStatus?.resumeAllowed ?? false,
        boardSlug: board.slug,
        missionId: mission.id,
        eventCount: options.runEvents?.length || 0,
      } })
    }
    if (url === '/api/formations/gate-profiles') {
      return respond({ profiles: [
        {
          profileId: 'output_absent',
          profileVersion: '1',
          displayName: 'Output excludes value',
          parameterName: 'value',
          parameterLabel: 'Forbidden text',
        },
        {
          profileId: 'output_contains',
          profileVersion: '1',
          displayName: 'Output contains value',
          parameterName: 'value',
          parameterLabel: 'Required text',
        },
      ] })
    }
    if (url === '/api/formations/boards') return respond({ boards: availableBoards.map(item => ({ id: item.id, slug: item.slug, title: item.title, rev: item.rev, etag: item.etag })) })
    if (url.includes('/changes')) {
      const refreshedBoard = options.sameBoardRefreshes?.shift()
      if (!refreshedBoard) return respond({ signal: { changed: false } })
      board = refreshedBoard
      currentLayout = { ...currentLayout, boardRev: refreshedBoard.rev }
      return respond({
        signal: {
          board: refreshedBoard.slug,
          changed: true,
          rev: refreshedBoard.rev,
          etag: refreshedBoard.etag,
        },
      })
    }
    if (url.endsWith('/layout')) return respond({ layout: currentLayout }, 'layout-etag')
    if (url.endsWith('/validation')) {
      return respond({ boardRev: board.rev, boardEtag: board.etag, errors: options.validation?.errors || [], warnings: options.validation?.warnings || [] })
    }
    if (url.includes('/api/formations/boards/')) {
      const requested = url.includes(`/boards/${board.slug}`)
        ? board
        : availableBoards.find(item => url.includes(`/boards/${item.slug}`)) || board
      return respond({ board: requested }, requested.etag)
    }
    if (url === '/api/agents') return respond({ agents: availableAgents })
    return respond({})
  }) as unknown as typeof fetch
  return patches
}

async function renderCockpit() {
  const utils = render(<FormationsCockpit />)
  await screen.findByTestId('formation-node-fmn_frame')
  return utils
}

let clickPointer = 40
/** Press and release a card without moving: a click, as the canvas sees one. */
function clickCard(target: HTMLElement) {
  const pointerId = clickPointer++
  fireEvent.pointerDown(target, { button: 0, pointerId, clientX: 300, clientY: 200 })
  fireEvent.pointerUp(window, { pointerId, clientX: 300, clientY: 200 })
}

async function openNodeWindow(target: HTMLElement, name: string) {
  clickCard(target)
  return screen.findByRole('dialog', { name })
}

describe('FormationsCockpit reference parity', () => {
  let patches: RecordedPatch[]

  beforeEach(() => {
    localStorage.clear()
    patches = installFetchMock()
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    window.history.replaceState(null, '', '/')
  })

  it('falls back to default text size outside a SessionProvider', async () => {
    await renderCockpit()
  })

  it('does not fabricate a starter board when no real boards exist', async () => {
    patches = installFetchMock({ emptyBoards: true })
    render(<FormationsCockpit />)
    expect(await screen.findByTestId('formations-empty-board')).toHaveTextContent('No persisted formation boards')
    expect(screen.getByTestId('board-picker')).toHaveTextContent('No boards')
    expect(screen.queryByText('Improve session search')).toBeNull()
    expect(screen.getByTestId('new-board')).toBeEnabled()
    expect(screen.getByTestId('new-formation')).toBeDisabled()
    expect(patches).toEqual([])
  })

  it('creates and selects a named blank board from the Formations top bar', async () => {
    patches = installFetchMock({ emptyBoards: true })
    render(<FormationsCockpit />)
    await screen.findByTestId('formations-empty-board')

    fireEvent.click(screen.getByTestId('new-board'))
    const dialog = await screen.findByRole('dialog', { name: 'Create board' })
    fireEvent.change(within(dialog).getByLabelText('Board name'), { target: { value: 'Release Plan' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create board' }))

    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveValue('release-plan'))
    expect(screen.getByTestId('board-picker')).toHaveTextContent('Release Plan')
    expect(screen.getByTestId('formations-empty-board')).toHaveTextContent('This board is empty')
    expect(recordedMutations).toContainEqual({ method: 'POST', url: '/api/formations/boards' })
  })

  it('renames the selected board through the top-bar board controls', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Rename board' }))
    const dialog = await screen.findByRole('dialog', { name: 'Rename board' })
    expect(screen.getByTestId('board-picker')).toBeDisabled()
    const input = within(dialog).getByLabelText('Board name')
    expect(input).toHaveValue('Test board')
    fireEvent.change(input, { target: { value: 'Delivery map' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save board name' }))

    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveTextContent('Delivery map'))
    expect(patches.some(patch => patch.body.title === 'Delivery map')).toBe(true)
  })

  it('archives a board only after explicit confirmation', async () => {
    await renderCockpit()
    const trigger = screen.getByRole('button', { name: 'Delete board' })
    fireEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: 'Delete board' })
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(dialog).toHaveTextContent('archived')
    expect(recordedMutations.some(mutation => mutation.method === 'DELETE')).toBe(false)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive board' }))
    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveTextContent('No boards'))
    expect(recordedMutations).toContainEqual({ method: 'DELETE', url: '/api/formations/boards/test-board' })
  })

  it('restores board-dialog trigger focus after Escape', async () => {
    await renderCockpit()
    const trigger = screen.getByTestId('new-board')
    fireEvent.click(trigger)
    expect(await screen.findByLabelText('Board name')).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create board' })).toBeNull())
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  it('rechecks note drafts before creating a board from an open dialog', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Board notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the board' })
    fireEvent.click(screen.getByTestId('new-board'))
    const dialog = await screen.findByRole('dialog', { name: 'Create board' })
    fireEvent.change(within(dialog).getByLabelText('Board name'), { target: { value: 'Should not create' } })
    fireEvent.change(boardNote, { target: { value: 'Draft made after dialog opened' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create board' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create board' })).toBeNull())
    expect(boardNote).toHaveValue('Draft made after dialog opened')
    expect(recordedMutations.some(mutation => mutation.method === 'POST' && mutation.url === '/api/formations/boards')).toBe(false)
  })

  it('rechecks note drafts before archiving from an open dialog', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Board notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the board' })
    fireEvent.click(screen.getByRole('button', { name: 'Delete board' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete board' })
    fireEvent.change(boardNote, { target: { value: 'Draft made after delete opened' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive board' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Delete board' })).toBeNull())
    expect(boardNote).toHaveValue('Draft made after delete opened')
    expect(recordedMutations.some(mutation => mutation.method === 'DELETE')).toBe(false)
  })

  it('shows mixed-author note threads in note windows and replies without overwriting', async () => {
    patches = installFetchMock({
      boardNotes: {
        board: [noteEntry('nte_board', 'human:ui', 'Preserve the API contract.')],
        elements: [{
          nodeId: 'fmn_frame',
          entries: [
            noteEntry('nte_operator', 'human:ui', 'Builder owns this element.'),
            noteEntry('nte_agent', 'agent:archon', 'Staffed Mason as the lead.'),
          ],
        }],
      },
    })
    await renderCockpit()

    const preview = screen.getByRole('note', { name: 'Notes for Frame' })
    expect(preview.closest('[data-testid="note-layer"]')).not.toBeNull()
    expect(screen.getByTestId('formation-node-fmn_frame')).not.toContainElement(preview)
    expect(preview).toHaveClass('note-sticky-agent')
    expect(within(preview).getByText('archon')).toHaveClass('note-author', 'note-author-agent')
    expect(preview).toHaveTextContent('+1 earlier')
    expect(preview).toHaveTextContent('Staffed Mason as the lead.')
    expect(screen.getByTestId('formation-node-fmn_frame')).toHaveClass('has-note')

    fireEvent.click(within(preview).getByRole('button', { name: 'Open the note thread for Frame' }))
    const noteWindow = await screen.findByRole('dialog', { name: 'notes for Frame' })
    const thread = within(noteWindow).getByRole('list', { name: 'Note thread for Frame' })
    expect(within(thread).getAllByRole('listitem').map(item => item.textContent)).toEqual([
      expect.stringContaining('Builder owns this element.'),
      expect.stringContaining('Staffed Mason as the lead.'),
    ])
    expect(screen.getByTestId('note-entry-nte_operator')).toHaveClass('note-entry-human')
    expect(screen.getByTestId('note-entry-nte_agent')).toHaveClass('note-entry-agent')
    expect(within(screen.getByTestId('note-entry-nte_operator')).getByText('operator')).toHaveClass('note-author', 'note-author-human')
    expect(within(screen.getByTestId('note-entry-nte_agent')).getByText('archon')).toHaveClass('note-author', 'note-author-agent')
    expect(within(screen.getByTestId('note-entry-nte_agent')).queryByRole('button')).toBeNull()

    const reply = within(noteWindow).getByRole('textbox', { name: 'Note for Frame' })
    await waitFor(() => expect(reply).toHaveFocus())
    fireEvent.change(reply, { target: { value: 'Add a reviewer too.' } })
    fireEvent.click(within(noteWindow).getByRole('button', { name: 'Reply' }))
    await waitFor(() => expect(within(thread).getAllByRole('listitem')).toHaveLength(3))
    expect(reply).toHaveValue('')
    const noteCall = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url).endsWith('/notes') && init?.method === 'PATCH')
    expect(JSON.parse(String(noteCall?.[1]?.body))).toEqual({ target: 'fmn_frame', action: 'append', text: 'Add a reviewer too.', author: 'human:ui' })

    fireEvent.click(within(screen.getByTestId('note-entry-nte_operator')).getByRole('button', { name: /^Edit your note/ }))
    expect(reply).toHaveValue('Builder owns this element.')
    fireEvent.change(reply, { target: { value: 'Builder and reviewer own this element.' } })
    fireEvent.click(within(noteWindow).getByRole('button', { name: 'Save edit' }))
    await waitFor(() => expect(screen.getByTestId('note-entry-nte_operator')).toHaveTextContent('Builder and reviewer own this element.'))
    expect(screen.getByTestId('note-entry-nte_operator')).toHaveTextContent('edited')
    expect(screen.getByTestId('note-entry-nte_agent')).toHaveTextContent('Staffed Mason as the lead.')

    fireEvent.click(screen.getByRole('button', { name: 'Board notes' }))
    const boardWindow = await screen.findByRole('dialog', { name: 'board notes' })
    expect(noteWindow).toBeInTheDocument()
    const boardThread = within(boardWindow).getByRole('list', { name: 'Board note thread' })
    fireEvent.click(within(boardThread).getByRole('button', { name: /^Delete your note/ }))
    await waitFor(() => expect(within(boardWindow).queryByRole('list', { name: 'Board note thread' })).toBeNull())
    expect(boardWindow).toHaveTextContent('No notes yet')

    fireEvent.click(within(noteWindow).getByRole('button', { name: 'Close notes for Frame' }))
    expect(screen.queryByRole('dialog', { name: 'notes for Frame' })).toBeNull()
    expect(screen.getByRole('dialog', { name: 'board notes' })).toBeInTheDocument()
  })

  it('adds a note to a node in one action from its note pin', async () => {
    await renderCockpit()
    const judge = screen.getByTestId('formation-node-fmn_judge')
    expect(judge).not.toHaveClass('has-note')

    fireEvent.click(within(judge).getByRole('button', { name: 'Add note for Judge' }))
    const noteWindow = await screen.findByRole('dialog', { name: 'notes for Judge' })
    const note = within(noteWindow).getByRole('textbox', { name: 'Note for Judge' })
    await waitFor(() => expect(note).toHaveFocus())
    fireEvent.change(note, { target: { value: 'In this step a judge checks the release evidence.' } })
    fireEvent.click(within(judge).getByRole('button', { name: 'Add note for Judge' }))
    expect(note).toHaveValue('In this step a judge checks the release evidence.')
    expect(screen.getAllByRole('dialog', { name: 'notes for Judge' })).toHaveLength(1)
    const add = within(noteWindow).getByRole('button', { name: 'Add note' })
    await waitFor(() => expect(add).toBeEnabled())
    fireEvent.click(add)

    await waitFor(() => expect(judge).toHaveClass('has-note'))
    expect(screen.getByRole('note', { name: 'Notes for Judge' })).toHaveTextContent('In this step a judge checks the release evidence.')
    expect(screen.getByRole('note', { name: 'Notes for Judge' })).toHaveClass('note-sticky-human')
  })

  it('switches canvas notes between hidden, preview and full, and remembers the choice', async () => {
    patches = installFetchMock({
      boardNotes: {
        elements: [{
          nodeId: 'fmn_frame',
          entries: [
            noteEntry('nte_operator', 'human:ui', 'In this step a single agent composes a research report.'),
            noteEntry('nte_agent', 'agent:archon', 'Staffed Mason as the lead.'),
          ],
        }],
      },
    })
    const { unmount } = await renderCockpit()
    const switcher = screen.getByRole('radiogroup', { name: 'Notes on the canvas' })
    expect(within(switcher).getByRole('radio', { name: 'Preview' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('note', { name: 'Notes for Frame' })).not.toHaveTextContent('composes a research report')

    fireEvent.click(within(switcher).getByRole('radio', { name: 'Full notes' }))
    const full = screen.getByRole('note', { name: 'Notes for Frame' })
    expect(full).toHaveTextContent('In this step a single agent composes a research report.')
    expect(full).toHaveTextContent('Staffed Mason as the lead.')
    expect(within(full).getAllByRole('listitem')).toHaveLength(2)
    expect(localStorage.getItem('chrote-formations-notes-mode')).toBe('full')

    fireEvent.click(within(switcher).getByRole('radio', { name: 'Hide notes' }))
    expect(screen.queryByRole('note', { name: 'Notes for Frame' })).toBeNull()
    expect(within(screen.getByTestId('formation-node-fmn_frame')).getByRole('button', { name: 'Open notes for Frame' })).toBeInTheDocument()

    unmount()
    await renderCockpit()
    expect(screen.getByRole('radio', { name: 'Hide notes' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByRole('note', { name: 'Notes for Frame' })).toBeNull()
  })

  it('preserves a local note draft and offers an explicit reload after a repeated conflict', async () => {
    patches = installFetchMock({ boardNotes: { board: [noteEntry('nte_server', 'agent:archon', 'Server version')] }, notePatchConflict: true })
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Board notes' }))
    const boardWindow = await screen.findByRole('dialog', { name: 'board notes' })
    const boardNote = within(boardWindow).getByRole('textbox', { name: 'Note for the board' })
    fireEvent.change(boardNote, { target: { value: 'Local draft' } })
    fireEvent.click(within(boardWindow).getByRole('button', { name: 'Reply' }))

    expect(await within(boardWindow).findByRole('alert')).toHaveTextContent('Shared notes changed')
    expect(boardNote).toHaveValue('Local draft')
    expect(vi.mocked(fetch).mock.calls.filter(([url, init]) => String(url).endsWith('/notes') && init?.method === 'PATCH')).toHaveLength(2)
    expect(within(boardWindow).getByRole('button', { name: 'Reload shared notes (discard local draft)' })).toBeEnabled()
  })

  it('protects unsaved notes from board switches, creation, and deletion', async () => {
    const second = { ...makeBoard(), id: 'board-2', slug: 'second-board', title: 'Second board', etag: 'board-2-etag' }
    patches = installFetchMock({ boards: [makeBoard(), second] })
    await renderCockpit()

    fireEvent.click(screen.getByRole('button', { name: 'Board notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the board' })
    fireEvent.change(boardNote, { target: { value: 'Unsaved local context' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close board notes' }))
    expect(screen.queryByRole('dialog', { name: 'board notes' })).toBeNull()
    fireEvent.change(screen.getByTestId('board-picker'), { target: { value: 'second-board' } })

    expect(screen.getByTestId('board-picker')).toHaveValue('test-board')
    const reopened = await screen.findByRole('dialog', { name: 'board notes' })
    expect(within(reopened).getByRole('alert')).toHaveTextContent('Save the current notes before leaving this board')
    expect(within(reopened).getByRole('textbox', { name: 'Note for the board' })).toHaveValue('Unsaved local context')
    fireEvent.click(screen.getByRole('button', { name: 'Delete board' }))
    expect(screen.queryByRole('dialog', { name: 'Delete board' })).toBeNull()
    fireEvent.click(screen.getByTestId('new-board'))
    expect(screen.queryByRole('dialog', { name: 'Create board' })).toBeNull()
  })

  it('keeps loading agent dialogs focused and restores their trigger on Escape', async () => {
    let releaseDetail: (() => void) | undefined
    const detailGate = new Promise<void>(resolve => { releaseDetail = resolve })
    patches = installFetchMock({ agentDetailGate: detailGate })
    await renderCockpit()
    const trigger = screen.getByRole('button', { name: 'Edit Mason' })
    fireEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: 'Edit agent' })
    expect(within(dialog).getByRole('button', { name: 'Close agent editor' })).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit agent' })).toBeNull())
    await waitFor(() => expect(trigger).toHaveFocus())
    await act(async () => { releaseDetail?.(); await detailGate })
  })

  it('shows Codex role presets beside Claude personas and persists UI overrides', async () => {
    const presetAgents: AgentProjection[] = [
      { id: 'claude-existing', displayName: 'Claude Existing', kind: 'builder', harnessDefault: 'claude-code', liveness: 'offline', assignable: true },
      ...['scout', 'planner', 'builder', 'judge', 'orchestrator', 'debugger', 'reviewer'].map(role => ({
        id: `codex-${role}`,
        displayName: `Codex ${role[0].toUpperCase()}${role.slice(1)}`,
        kind: role,
        tags: [`role:${role}`],
        harnessDefault: 'openai-codex',
        liveness: 'offline',
        assignable: true,
        preset: true,
      })),
    ]
    patches = installFetchMock({ agents: presetAgents })
    await renderCockpit()

    const roster = screen.getByLabelText('Agent roster')
    expect(within(roster).getByText('Codex')).toBeInTheDocument()
    expect(within(roster).getByText('Claude')).toBeInTheDocument()
    for (const role of ['Scout', 'Planner', 'Builder', 'Judge', 'Orchestrator', 'Debugger', 'Reviewer']) {
      expect(within(roster).getByText(`Codex ${role}`)).toBeInTheDocument()
    }

    const editTrigger = within(roster).getByRole('button', { name: 'Edit Codex Builder' })
    fireEvent.click(editTrigger)
    const dialog = await screen.findByRole('dialog', { name: 'Edit agent preset' })
    expect(dialog).toHaveAttribute('data-testid', 'persona-editor')
    expect(await within(dialog).findByLabelText('Agent display name')).toHaveFocus()
    fireEvent.change(within(dialog).getByLabelText('Agent display name'), { target: { value: 'Repository Builder' } })
    fireEvent.change(within(dialog).getByLabelText('Agent capabilities'), { target: { value: 'implement, test, refactor' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save agent override' }))

    await waitFor(() => expect(within(roster).getByText('Repository Builder')).toBeInTheDocument())
    await waitFor(() => expect(editTrigger).toHaveFocus())
    expect(recordedMutations).toContainEqual({ method: 'PATCH', url: '/api/agents/codex-builder' })
  })

  it('shows only assignable persona cards in the formation staffing roster', async () => {
    await renderCockpit()
    const roster = screen.getByTestId('agent-roster')
    expect(screen.getByTestId('roster-count')).toHaveTextContent(/^2 · 1 on board$/)
    expect(roster).toHaveTextContent('Mason')
    expect(roster).toHaveTextContent('Hazel')
    expect(roster).not.toHaveTextContent('scratch')
  })

  it('renders judge connections as wires anchored on the gate socket', async () => {
    const { container } = await renderCockpit()
    await waitFor(() => {
      const judgeWires = container.querySelectorAll('path.wire.judge')
      expect(judgeWires.length).toBe(2)
    })
    expect(container.querySelector('[data-gate-judge-socket="gate_review"]')).toBeTruthy()
    expect(screen.getByTestId('gate-node-gate_review').className).toContain('hasjudge')
  })

  it('renders a non-executing Tool with its frozen identity, parameters, and declared work ports', async () => {
    patches = installFetchMock({ boards: [makeToolBoard()] })

    const { container } = await renderCockpit()
    const card = await screen.findByTestId('tool-node-tool_normalize')

    expect(card).toHaveClass('toolcard')
    expect(card).not.toHaveClass('formation')
    expect(card).not.toHaveClass('missioncard')
    expect(card).not.toHaveClass('gatecard')
    expect(card).toHaveAttribute('data-kind', 'tool')
    expect(card).toHaveAttribute('data-node', 'tool_normalize')
    expect(card).toHaveAttribute('data-execution-state', 'unavailable')
    expect(screen.getByTestId('formations-world')).toContainElement(card)
    expect(card).toHaveStyle({ left: '1680px', top: '364px' })
    expect(card).toHaveTextContent('Normalize report')
    expect(card).toHaveTextContent('json.normalize@1')
    expect(card).toHaveTextContent('execution unavailable')
    expect(card).toHaveTextContent('Report')
    expect(card).toHaveTextContent('Normalized report')
    expect(container.querySelector('[data-port-in="tool_normalize:port_tool_input"]')).toBeTruthy()
    expect(container.querySelector('[data-port-out="tool_normalize:port_tool_output"]')).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId('formation-wire-edge_tool_chain')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByTestId('formation-wire-edge_tool_gate')).toBeInTheDocument())
    expect(within(card).getAllByRole('button').map(button => button.getAttribute('aria-label'))).toEqual([
      'Add note for Normalize report',
      'Inspect Tool Normalize report',
    ])
    fireEvent.contextMenu(card)
    expect(screen.queryByRole('menu')).toBeNull()
    expect(patches).toEqual([])
    expect(recordedMutations).toEqual([])
  })

  it('opens a read-only Tool inspector with the complete frozen projection', async () => {
    patches = installFetchMock({ boards: [makeToolBoard()] })
    await renderCockpit()

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Tool Normalize report' }))
    const dialog = await screen.findByRole('dialog', { name: 'Tool details: Normalize report' })

    expect(dialog).toHaveTextContent('tool_normalize')
    expect(dialog).toHaveTextContent('json.normalize@1')
    expect(dialog).toHaveTextContent('execution unavailable')

    const parameter = within(dialog).getByTestId('tool-parameter-mode')
    expect(parameter).toHaveTextContent('mode')
    expect(parameter).toHaveTextContent('string')
    expect(parameter).toHaveTextContent('strict')

    const input = within(dialog).getByTestId('tool-port-port_tool_input')
    expect(input).toHaveAttribute('data-direction', 'input')
    expect(input).toHaveTextContent('port_tool_input')
    expect(input).toHaveTextContent('input')
    expect(input).toHaveTextContent('Report')
    expect(input).toHaveTextContent('work')
    expect(input).toHaveTextContent('application/json')
    expect(input).toHaveTextContent('required=true')
    expect(input).toHaveTextContent('role=data')

    const output = within(dialog).getByTestId('tool-port-port_tool_output')
    expect(output).toHaveAttribute('data-direction', 'output')
    expect(output).toHaveTextContent('port_tool_output')
    expect(output).toHaveTextContent('output')
    expect(output).toHaveTextContent('Normalized report')
    expect(output).toHaveTextContent('work')
    expect(output).toHaveTextContent('application/json')
    expect(output).not.toHaveTextContent('required')
    expect(output).not.toHaveTextContent('data')
    expect(within(dialog).getAllByRole('button').map(button => button.getAttribute('aria-label'))).toEqual([
      'Close Tool details',
    ])

    fireEvent.click(within(dialog).getByRole('button', { name: 'Close Tool details' }))
    expect(dialog).not.toBeInTheDocument()
    expect(patches).toEqual([])
    expect(recordedMutations).toEqual([])
  })

  it('keeps an inspection-readable invalid Tool visible when optional arrays decode as null', async () => {
    const base = makeBoard()
    const degradedTool = {
      ...tool,
      params: null,
      inputs: [{ ...tool.inputs[0], acceptedMediaTypes: null }],
      outputs: null,
    } as unknown as typeof tool
    patches = installFetchMock({ boards: [{ ...base, schema: 2, tools: [degradedTool] }] })

    await renderCockpit()
    const card = await screen.findByTestId('tool-node-tool_normalize')
    expect(card).toHaveTextContent('Normalize report')
    expect(card).toHaveTextContent('json.normalize@1')
    expect(card).toHaveTextContent('execution unavailable')
    expect(card).toHaveTextContent('Report')

    fireEvent.click(within(card).getByRole('button', { name: 'Inspect Tool Normalize report' }))
    const dialog = await screen.findByRole('dialog', { name: 'Tool details: Normalize report' })
    expect(dialog).toHaveTextContent('tool_normalize')
    expect(within(dialog).getByTestId('tool-port-port_tool_input')).toHaveTextContent('media not declared')
    expect(within(dialog).queryByTestId('tool-parameter-mode')).toBeNull()
    expect(recordedMutations).toEqual([])
  })

  it('preserves readable Tool projection order and distinguishes false from unset input metadata', async () => {
    const base = makeBoard()
    const inspectionTool = {
      ...tool,
      params: { zeta: true, alpha: 7 },
      inputs: [
        {
          ...tool.inputs[0],
          id: 'port_false_input',
          name: 'false_input',
          label: 'False input',
          acceptedMediaTypes: ['application/json', 'text/plain'],
          required: false,
          role: undefined,
        },
        {
          ...tool.inputs[0],
          id: 'port_unset_input',
          name: 'unset_input',
          label: 'Unset input',
          acceptedMediaTypes: ['text/markdown', 'application/json'],
          required: undefined,
          role: undefined,
        },
      ],
      outputs: [
        { ...tool.outputs[0], id: 'port_first_output', name: 'first_output', label: 'First output' },
        { ...tool.outputs[0], id: 'port_second_output', name: 'second_output', label: 'Second output' },
      ],
    } as unknown as typeof tool
    patches = installFetchMock({ boards: [{ ...base, schema: 2, tools: [inspectionTool] }] })

    await renderCockpit()
    const card = await screen.findByTestId('tool-node-tool_normalize')
    expect(Array.from(card.querySelectorAll('.tool-ports.inputs .tool-port-label'), node => node.textContent)).toEqual([
      'False input',
      'Unset input',
    ])
    expect(Array.from(card.querySelectorAll('.tool-ports.outputs .tool-port-label'), node => node.textContent)).toEqual([
      'First output',
      'Second output',
    ])

    fireEvent.click(within(card).getByRole('button', { name: 'Inspect Tool Normalize report' }))
    const dialog = await screen.findByRole('dialog', { name: 'Tool details: Normalize report' })
    expect(within(dialog).getAllByTestId(/^tool-parameter-/).map(row => row.getAttribute('data-testid'))).toEqual([
      'tool-parameter-alpha',
      'tool-parameter-zeta',
    ])
    const alphaParameter = within(dialog).getByTestId('tool-parameter-alpha')
    expect(within(alphaParameter).getByText('alpha')).toBeInTheDocument()
    expect(within(alphaParameter).getByText('integer')).toBeInTheDocument()
    expect(within(alphaParameter).getByText('7')).toBeInTheDocument()
    const zetaParameter = within(dialog).getByTestId('tool-parameter-zeta')
    expect(within(zetaParameter).getByText('zeta')).toBeInTheDocument()
    expect(within(zetaParameter).getByText('boolean')).toBeInTheDocument()
    expect(within(zetaParameter).getByText('true')).toBeInTheDocument()

    const projectedPorts = within(dialog).getAllByTestId(/^tool-port-/)
    expect(projectedPorts.map(port => port.getAttribute('data-testid'))).toEqual([
      'tool-port-port_false_input',
      'tool-port-port_unset_input',
      'tool-port-port_first_output',
      'tool-port-port_second_output',
    ])
    expect(projectedPorts[0]).toHaveTextContent('application/json · text/plain')
    expect(projectedPorts[0]).toHaveTextContent('required=false')
    expect(projectedPorts[0]).toHaveTextContent('role=unset')
    expect(projectedPorts[1]).toHaveTextContent('text/markdown · application/json')
    expect(projectedPorts[1]).toHaveTextContent('required=unset')
    expect(projectedPorts[1]).toHaveTextContent('role=unset')
    expect(patches).toEqual([])
    expect(recordedMutations).toEqual([])
  })

  it('does not reopen a Tool inspector when the same board removes and later restores that node ID', async () => {
    const initialBoard = makeToolBoard()
    const refreshes: TestBoard[] = []
    const changePolls: Array<() => void> = []
    let timerId = 0
    vi.spyOn(window, 'setInterval').mockImplementation((handler, timeout) => {
      timerId += 1
      if (timeout === 600) changePolls.push(() => handler())
      return timerId as unknown as ReturnType<typeof setInterval>
    })
    const withoutTool = {
      ...initialBoard,
      rev: initialBoard.rev + 1,
      etag: 'board-without-tool-etag',
      tools: [upstreamTool],
      connections: initialBoard.connections.filter(connection => (
        !connection.from.startsWith('tool_normalize:') && !connection.to.startsWith('tool_normalize:')
      )),
    }
    const restoredTool = {
      ...initialBoard,
      rev: initialBoard.rev + 2,
      etag: 'board-restored-tool-etag',
      tools: [upstreamTool, { ...tool, title: 'Restored normalize report' }],
    }
    patches = installFetchMock({ boards: [initialBoard], sameBoardRefreshes: refreshes })
    await renderCockpit()
    await waitFor(() => expect(changePolls).toHaveLength(1))

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Tool Normalize report' }))
    await screen.findByRole('dialog', { name: 'Tool details: Normalize report' })

    refreshes.push(withoutTool)
    await act(async () => { changePolls[0]() })
    await waitFor(() => {
      expect(screen.queryByTestId('tool-node-tool_normalize')).toBeNull()
      expect(screen.queryByRole('dialog', { name: 'Tool details: Normalize report' })).toBeNull()
    })
    await waitFor(() => expect(changePolls.length).toBeGreaterThan(1))

    refreshes.push(restoredTool)
    await act(async () => { changePolls[changePolls.length - 1]() })
    const restoredCard = await screen.findByTestId('tool-node-tool_normalize')
    expect(restoredCard).toHaveTextContent('Restored normalize report')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(patches).toEqual([])
    expect(recordedMutations).toEqual([])
  })

  it('creates a Mission with no optional input and rejects only an unsafe Bead ID', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockReturnValue(1)
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    const menu = await screen.findByRole('menu', { name: 'New' })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mission' }))

    const dialog = await screen.findByRole('dialog', { name: 'Create mission' })
    expect(menu).not.toBeInTheDocument()
    expect(patches.filter(patch => patch.body.createMission)).toEqual([])

    fireEvent.change(screen.getByLabelText('Mission Bead ID'), { target: { value: 'Home-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mission' }))
    expect(await screen.findByText('Enter a Beads issue ID such as ctx-ug7.25, or leave it blank.')).toBeInTheDocument()
    expect(screen.getByLabelText('Mission Bead ID')).toHaveAttribute('aria-invalid', 'true')
    expect(patches.filter(patch => patch.body.createMission)).toEqual([])

    fireEvent.change(screen.getByLabelText('Mission Bead ID'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mission' }))

    await waitFor(() => {
      const create = patches.find(patch => patch.body.createMission)
      expect(create?.body.createMission).toEqual({
        title: 'New mission',
        goal: '',
        beadId: '',
        x: 1260,
        y: 252,
      })
    })
    expect(dialog).not.toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.some(patch => (patch.body.deleteMission as { id?: string } | undefined)?.id === 'mis_created')).toBe(true)
    })

    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('mission-node-mis_created')).toHaveTextContent('New mission')
  })

  it('creates a Mission with a project Bead ID when one is given', async () => {
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mission' }))
    await screen.findByRole('dialog', { name: 'Create mission' })
    fireEvent.change(screen.getByLabelText('Mission title'), { target: { value: '  Plan release  ' } })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: '  Ship reduced candidate  ' } })
    fireEvent.change(screen.getByLabelText('Mission Bead ID'), { target: { value: ' home-vdki.34.1 ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mission' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.createMission)?.body.createMission).toMatchObject({
        title: 'Plan release',
        goal: 'Ship reduced candidate',
        beadId: 'home-vdki.34.1',
      })
    })
  })

  it('cancels Mission creation with Cancel and Escape without mutation', async () => {
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement

    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mission' }))
    await screen.findByRole('dialog', { name: 'Create mission' })
    fireEvent.change(screen.getByLabelText('Mission title'), { target: { value: 'Discard me' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel mission creation' }))
    expect(screen.queryByRole('dialog', { name: 'Create mission' })).toBeNull()

    fireEvent.contextMenu(viewport, { clientX: 360, clientY: 360 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mission' }))
    await screen.findByRole('dialog', { name: 'Create mission' })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: 'Discard this too' } })
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create mission' })).toBeNull())

    expect(patches.filter(patch => patch.body.createMission)).toEqual([])
  })

  it('retains the Mission draft after the API rejects creation', async () => {
    patches = installFetchMock({ missionCreateFailure: true })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mission' }))
    await screen.findByRole('dialog', { name: 'Create mission' })

    fireEvent.change(screen.getByLabelText('Mission title'), { target: { value: 'Plan release' } })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: 'Ship reduced candidate' } })
    fireEvent.change(screen.getByLabelText('Mission Bead ID'), { target: { value: 'ctx-ug7.25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mission' }))

    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Mission create failed')
    expect(screen.getByRole('dialog', { name: 'Create mission' })).toBeInTheDocument()
    expect(screen.getByLabelText('Mission title')).toHaveValue('Plan release')
    expect(screen.getByLabelText('Mission goal')).toHaveValue('Ship reduced candidate')
    expect(screen.getByLabelText('Mission Bead ID')).toHaveValue('ctx-ug7.25')
  })

  it('adopts a created gate layout without issuing a stale follow-up layout patch', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    fireEvent.change(within(dialog).getByLabelText('Gate criterion'), { target: { value: 'Looks right' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))

    const created = await screen.findByTestId('gate-node-gate_created')
    await waitFor(() => {
      expect(created).toHaveStyle({ left: '1344px', top: '784px' })
    })
    expect(patches.filter(patch => patch.url.endsWith('/layout'))).toEqual([])
  })

  it('creates a code Gate from a backend-registered exact profile tuple', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))

    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    fireEvent.click(within(dialog).getByLabelText('Code kind'))
    fireEvent.click(within(dialog).getByLabelText('Human kind'))
    fireEvent.change(within(dialog).getByLabelText('Evaluator profile'), { target: { value: 'output_contains@1' } })
    fireEvent.change(within(dialog).getByLabelText('Required text'), { target: { value: 'LINT OK' } })
    fireEvent.change(within(dialog).getByLabelText('Gate criterion'), { target: { value: 'Lint passes clean' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))

    await waitFor(() => {
      const create = patches.find(patch => patch.body.createGate)?.body.createGate
      expect(create).toMatchObject({
        kinds: ['code'],
        criterion: 'Lint passes clean',
        check: 'output_contains',
        checkVersion: '1',
        checkValue: 'LINT OK',
      })
    })
  })

  it('starts a new Gate as a human gate and saves it with no other input', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    expect(within(dialog).getByLabelText('Human kind')).toBeChecked()
    expect(within(dialog).getByLabelText('Judge kind')).not.toBeChecked()
    expect(within(dialog).getByLabelText('Code kind')).not.toBeChecked()
    expect(within(dialog).queryByLabelText('Evaluator profile')).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.createGate)?.body.createGate).toMatchObject({
        title: 'Review gate',
        kinds: ['human'],
        criterion: '',
        check: '',
        checkVersion: '',
        checkValue: '',
      })
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create gate' })).toBeNull())

    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('gate-node-gate_created')).toHaveTextContent('Review gate')
  })

  it('lets a Gate leave its evaluator profile for later', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    fireEvent.click(within(dialog).getByLabelText('Code kind'))
    fireEvent.change(within(dialog).getByLabelText('Evaluator profile'), { target: { value: '' } })
    expect(within(dialog).getByLabelText('Value')).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.createGate)?.body.createGate).toMatchObject({ check: '', checkVersion: '', checkValue: '' })
    })
  })

  it('creates a formation with no optional input and reloads it intact', async () => {
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Solo formation' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.createFormation)?.body.createFormation).toMatchObject({ type: 'solo' })
    })
    expect(await screen.findByTestId('formation-node-fmn_created')).toBeInTheDocument()

    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('formation-node-fmn_created')).toBeInTheDocument()
  })

  it('marks draft nodes with what a run would still need', async () => {
    patches = installFetchMock({
      validation: {
        errors: [
          { code: 'unstaffed_slot', nodeId: 'fmn_judge', message: 'formation "fmn_judge" slot "Judge" (slot_judge) needs an agent' },
          { code: 'dangling_connection', nodeId: 'edge_frame_gate', message: 'connection "edge_frame_gate" has a broken endpoint' },
        ],
        warnings: [{ code: 'mission_not_runnable', nodeId: 'mis_showcase', message: 'mission "mis_showcase" has no outgoing connection' }],
      },
    })
    await renderCockpit()
    const judgeMarker = await screen.findByTestId('draft-marker-fmn_judge')
    expect(judgeMarker).toHaveTextContent('draft')
    expect(judgeMarker).toHaveAttribute('title', 'slot "Judge" (slot_judge) needs an agent')
    expect(screen.getByTestId('formation-node-fmn_judge')).toHaveClass('is-draft')
    expect(screen.getByTestId('draft-marker-mis_showcase')).toBeInTheDocument()
    expect(screen.getByTestId('draft-marker-fmn_frame')).toHaveAttribute('title', 'connection "edge_frame_gate" has a broken endpoint')
    expect(screen.getByTestId('draft-marker-gate_review')).toBeInTheDocument()
    expect(screen.queryByTestId('admission-findings')).toBeNull()
  })

  it('highlights every node a rejected run start names', async () => {
    const findings = [
      { code: 'unstaffed_slot', nodeId: 'fmn_frame', message: 'formation "fmn_frame" slot "Worker" (slot_worker) needs an agent' },
      { code: 'gate_not_routable', nodeId: 'gate_review', message: 'gate "gate_review" needs forbidden text for code check output_absent@1' },
    ]
    patches = installFetchMock({ runStartFindings: findings, validation: { errors: findings, warnings: [] } })
    await renderCockpit()
    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const dialog = await screen.findByRole('dialog', { name: 'Start mission' })
    fireEvent.change(within(dialog).getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Implement the requested change' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start mission' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('The run needs 2 fixes before it can start')
    const panel = await screen.findByTestId('admission-findings')
    expect(panel).toHaveTextContent('Run needs 2 fixes')
    expect(panel).toHaveTextContent('Frame')
    expect(panel).toHaveTextContent('needs forbidden text')
    expect(screen.getByTestId('formation-node-fmn_frame')).toHaveClass('admission-blocked')
    expect(screen.getByTestId('gate-node-gate_review')).toHaveClass('admission-blocked')
    expect(screen.getByTestId('draft-marker-gate_review')).toHaveTextContent('needs fix')
    expect(screen.getByTestId('mission-node-mis_showcase')).not.toHaveClass('admission-blocked')

    fireEvent.click(within(panel).getByRole('button', { name: 'Dismiss run findings' }))
    expect(screen.queryByTestId('admission-findings')).toBeNull()
    expect(screen.getByTestId('formation-node-fmn_frame')).not.toHaveClass('admission-blocked')
    expect(screen.getByTestId('draft-marker-gate_review')).toHaveTextContent('draft')
  })

  it('creates human and judge gates from the gate editor and reloads their kinds', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    expect(within(dialog).getByLabelText('Human kind')).toBeChecked()
    expect(within(dialog).getByLabelText('Human kind')).toBeDisabled()

    fireEvent.click(within(dialog).getByLabelText('Judge kind'))
    expect(within(dialog).getByText("Attach a judge formation from the gate's judge socket. A run needs one.")).toBeInTheDocument()
    fireEvent.click(within(dialog).getByLabelText('Code kind'))
    expect(within(dialog).getByLabelText('Evaluator profile')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByLabelText('Code kind'))
    expect(within(dialog).queryByLabelText('Evaluator profile')).toBeNull()
    fireEvent.change(within(dialog).getByLabelText('Gate title'), { target: { value: 'Framing review' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.createGate)?.body.createGate).toMatchObject({
        title: 'Framing review',
        kinds: ['human', 'formation'],
        check: '',
        checkVersion: '',
        checkValue: '',
      })
    })
    expect(await screen.findByTestId('gate-kinds-gate_created')).toHaveTextContent('humanjudge')

    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('gate-kinds-gate_created')).toHaveTextContent('humanjudge')
  })

  it('opens each node kind in a window by click, and a drag moves a card without opening it', async () => {
    await renderCockpit()
    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Mission · Showcase')
    expect(within(mission).getByText('Build the page')).toBeInTheDocument()
    expect(within(mission).getByText('home-7kc4.5')).toBeInTheDocument()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    expect(within(frame).getByText('Step 2 · Formation · orchestrated')).toBeInTheDocument()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    expect(within(review).getByText('Review the frame')).toBeInTheDocument()
    expect(Number(review.style.zIndex)).toBeGreaterThan(Number(mission.style.zIndex))

    // Clicking the mission card again raises its open window instead of opening another.
    clickCard(screen.getByTestId('mission-node-mis_showcase'))
    await waitFor(() => expect(Number(mission.style.zIndex)).toBeGreaterThan(Number(review.style.zIndex)))
    expect(screen.getAllByRole('dialog', { name: 'Mission · Showcase' })).toHaveLength(1)
    expect(patches).toEqual([])

    fireEvent.keyDown(review, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Gate · Review' })).toBeNull())
    const gateCard = screen.getByTestId('gate-node-gate_review')
    fireEvent.pointerDown(gateCard, { button: 0, pointerId: 9, clientX: 300, clientY: 200 })
    fireEvent.pointerMove(window, { pointerId: 9, clientX: 360, clientY: 240 })
    fireEvent.pointerUp(window, { pointerId: 9, clientX: 360, clientY: 240 })
    await waitFor(() => expect(patches.some(patch => patch.url.endsWith('/layout'))).toBe(true))
    expect(screen.queryByRole('dialog', { name: 'Gate · Review' })).toBeNull()
    expect(patches.filter(patch => !patch.url.endsWith('/layout'))).toEqual([])
  })

  it('states routes in words, follows them to other windows, and says where an unwired pass ends the run', async () => {
    await renderCockpit()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    const routes = within(within(review).getByRole('region', { name: 'Connections' })).getAllByRole('listitem').map(item => item.textContent)
    expect(routes).toEqual(['Fed by 2 Frame', 'Judged by 4 Judge', 'Pass → run ends here', 'Fail → the run blocks here'])
    fireEvent.click(within(review).getByRole('button', { name: 'Judged by 4 Judge' }))
    const judge = await screen.findByRole('dialog', { name: 'Formation · Judge' })
    expect(within(judge).getByRole('button', { name: 'Judges 3 Review' })).toBeInTheDocument()
    fireEvent.click(within(review).getByRole('button', { name: 'Fed by 2 Frame' }))
    const frame = await screen.findByRole('dialog', { name: 'Formation · Frame' })
    expect(within(frame).getByRole('button', { name: 'Fed by 1 Showcase' })).toBeInTheDocument()
    expect(within(frame).getByRole('button', { name: 'Feeds → 3 Review' })).toBeInTheDocument()
  })

  it('converts a code gate to a human gate in its window and undoes it', async () => {
    const codeBoard = makeBoard()
    codeBoard.gates = [{ ...gate, check: 'output_absent', checkVersion: '1', checkValue: 'complaint text' }]
    patches = installFetchMock({ boards: [codeBoard] })
    const { unmount } = await renderCockpit()
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('code')

    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    expect(within(review).getByText('Code check: output_absent@1 · complaint text')).toBeInTheDocument()
    fireEvent.click(within(review).getByRole('button', { name: 'Edit kinds' }))
    expect(within(review).getByLabelText('Evaluator profile')).toHaveValue('output_absent@1')
    expect(within(review).getByLabelText('Forbidden text')).toHaveValue('complaint text')
    fireEvent.click(within(review).getByLabelText('Human kind'))
    fireEvent.click(within(review).getByLabelText('Code kind'))
    fireEvent.click(within(review).getByRole('button', { name: 'Save kinds' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toEqual({
        id: 'gate_review', title: 'Review', kinds: ['human'], criterion: 'Review the frame', check: '', checkVersion: '', checkValue: '',
      })
    })
    await waitFor(() => expect(within(review).queryByRole('button', { name: 'Save kinds' })).toBeNull())
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('human')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toEqual({
        id: 'gate_review', title: 'Review', kinds: ['code'], criterion: 'Review the frame', check: 'output_absent', checkVersion: '1', checkValue: 'complaint text',
      })
    })

    fireEvent.click(within(review).getByRole('button', { name: 'Edit kinds' }))
    fireEvent.click(within(review).getByLabelText('Human kind'))
    fireEvent.click(within(review).getByLabelText('Code kind'))
    fireEvent.click(within(review).getByRole('button', { name: 'Save kinds' }))
    await waitFor(() => expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('human'))
    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('gate-kinds-gate_review')).toHaveTextContent('human')
  })

  it('edits a gate criterion in its window and restores a detached judge chain on undo', async () => {
    const judgedBoard = makeBoard()
    judgedBoard.gates = [{ ...gate, kinds: ['code', 'formation'] }]
    patches = installFetchMock({ boards: [judgedBoard] })
    await renderCockpit()
    expect(await screen.findByTestId('formation-wire-edge_judge_send')).toBeInTheDocument()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')

    fireEvent.click(within(review).getByRole('button', { name: 'Edit criterion' }))
    fireEvent.change(within(review).getByRole('textbox', { name: 'Criterion' }), { target: { value: 'The frame names **three** risks' } })
    fireEvent.click(within(review).getByRole('button', { name: 'Save criterion' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toMatchObject({ id: 'gate_review', criterion: 'The frame names **three** risks', kinds: ['formation', 'code'] }))
    expect((await within(review).findByText('three')).tagName).toBe('STRONG')
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toMatchObject({ criterion: 'Review the frame' }))

    fireEvent.click(within(review).getByRole('button', { name: 'Edit kinds' }))
    fireEvent.click(within(review).getByLabelText('Judge kind'))
    expect(within(review).getByRole('note')).toHaveTextContent('Saving detaches the current judge chain.')
    fireEvent.click(within(review).getByRole('button', { name: 'Save kinds' }))
    await waitFor(() => expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toMatchObject({ kinds: ['code'] }))
    await waitFor(() => expect(screen.queryByTestId('formation-wire-edge_judge_send')).toBeNull())
    expect(screen.getByTestId('gate-node-gate_review')).not.toHaveClass('hasjudge')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setGateJudge)?.body.setGateJudge).toEqual({ gateId: 'gate_review', chain: ['fmn_judge'] })
    })
    // The fields, with the judge kind, are restored just before the chain.
    const chainRestore = patches.findIndex(patch => patch.body.setGateJudge)
    expect((patches[chainRestore - 1].body.updateGate as { kinds?: string[] }).kinds).toContain('formation')
  })

  it('renames a formation in its window, undoes it, and the title survives reload', async () => {
    const { unmount } = await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    fireEvent.click(within(frame).getByRole('button', { name: 'Edit title' }))
    const input = within(frame).getByRole('textbox', { name: 'Title' })
    expect(input).toHaveValue('Frame')
    fireEvent.change(input, { target: { value: '  Map the territory ' } })
    fireEvent.submit(input)

    await waitFor(() => {
      expect(patches.find(patch => patch.body.updateFormation)?.body.updateFormation).toEqual({ id: 'fmn_frame', title: 'Map the territory' })
    })
    expect(await within(screen.getByTestId('formation-node-fmn_frame')).findByText('Map the territory')).toBeInTheDocument()
    expect(await screen.findByRole('dialog', { name: 'Formation · Map the territory' })).toBe(frame)
    expect(patches.filter(patch => patch.url.endsWith('/layout'))).toEqual([])

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateFormation).map(patch => patch.body.updateFormation)).toEqual([
        { id: 'fmn_frame', title: 'Map the territory' },
        { id: 'fmn_frame', title: 'Frame' },
      ])
    })

    fireEvent.click(within(frame).getByRole('button', { name: 'Edit title' }))
    fireEvent.change(within(frame).getByRole('textbox', { name: 'Title' }), { target: { value: 'Question peers' } })
    fireEvent.click(within(frame).getByRole('button', { name: 'Save title' }))
    await waitFor(() => expect(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Question peers')).toBeInTheDocument())

    unmount()
    await renderCockpit()
    expect(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Question peers')).toBeInTheDocument()
  })

  it('cancels a field edit with Escape without closing the window, and skips unchanged titles', async () => {
    await renderCockpit()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    fireEvent.click(within(review).getByRole('button', { name: 'Edit title' }))
    fireEvent.change(within(review).getByRole('textbox', { name: 'Title' }), { target: { value: 'Discard me' } })
    fireEvent.keyDown(within(review).getByRole('textbox', { name: 'Title' }), { key: 'Escape' })
    expect(within(review).queryByRole('textbox', { name: 'Title' })).toBeNull()
    expect(screen.getByRole('dialog', { name: 'Gate · Review' })).toBe(review)

    fireEvent.click(within(review).getByRole('button', { name: 'Edit title' }))
    fireEvent.click(within(review).getByRole('button', { name: 'Save title' }))
    fireEvent.click(within(review).getByRole('button', { name: 'Edit title' }))
    fireEvent.change(within(review).getByRole('textbox', { name: 'Title' }), { target: { value: 'Framing review' } })
    fireEvent.click(within(review).getByRole('button', { name: 'Save title' }))
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateGate).map(patch => patch.body.updateGate)).toEqual([{ id: 'gate_review', title: 'Framing review' }])
    })
  })

  it('edits a mission goal and Bead ID in its window, undoes it, and the change survives reload', async () => {
    const { unmount } = await renderCockpit()
    const win = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Mission · Showcase')

    fireEvent.click(within(win).getByRole('button', { name: 'Edit bead' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Bead' }), { target: { value: 'Not A Bead' } })
    fireEvent.click(within(win).getByRole('button', { name: 'Save bead' }))
    expect(await within(win).findByText('Enter a Beads issue ID such as ctx-ug7.25, or leave it blank.')).toBeInTheDocument()
    expect(patches.filter(patch => patch.body.updateMission)).toEqual([])
    fireEvent.change(within(win).getByRole('textbox', { name: 'Bead' }), { target: { value: '' } })
    fireEvent.click(within(win).getByRole('button', { name: 'Save bead' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateMission)?.body.updateMission).toEqual({ id: 'mis_showcase', beadId: '' }))

    fireEvent.click(within(win).getByRole('button', { name: 'Edit goal' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Goal' }), { target: { value: 'Draft a framing for review' } })
    fireEvent.keyDown(within(win).getByRole('textbox', { name: 'Goal' }), { key: 'Enter', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({ id: 'mis_showcase', goal: 'Draft a framing for review' })
    })
    await waitFor(() => expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Draft a framing for review'))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({ id: 'mis_showcase', goal: 'Build the page' })
    })
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({ id: 'mis_showcase', beadId: 'home-7kc4.5' })
    })

    fireEvent.click(within(win).getByRole('button', { name: 'Edit goal' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Goal' }), { target: { value: 'Map the territory first' } })
    fireEvent.click(within(win).getByRole('button', { name: 'Save goal' }))
    await waitFor(() => expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Map the territory first'))

    unmount()
    await renderCockpit()
    expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Map the territory first')
  })

  it('sets the input hint Start mission shows from the mission window, with undo', async () => {
    await renderCockpit()
    const win = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Mission · Showcase')
    fireEvent.click(within(win).getByRole('button', { name: 'Edit input hint' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Input hint' }), { target: { value: 'Paste the page sketch and its copy deck.' } })
    fireEvent.click(within(win).getByRole('button', { name: 'Save input hint' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateMission)?.body.updateMission).toEqual({ id: 'mis_showcase', inputHint: 'Paste the page sketch and its copy deck.' }))
    expect(await within(win).findByText('Paste the page sketch and its copy deck.')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const start = await screen.findByRole('dialog', { name: 'Start mission' })
    expect(within(start).getByText('Paste the page sketch and its copy deck.')).toBeInTheDocument()
    fireEvent.click(within(start).getByRole('button', { name: 'Cancel' }))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({ id: 'mis_showcase', inputHint: '' }))
  })

  it('edits the reference files of a mission and a gate in their windows, with undo', async () => {
    await renderCockpit()
    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Mission · Showcase')
    fireEvent.click(within(mission).getByRole('button', { name: 'Edit files' }))
    fireEvent.change(within(mission).getByRole('textbox', { name: 'Files' }), { target: { value: 'docs/sketch.md, docs/copy.md' } })
    fireEvent.click(within(mission).getByRole('button', { name: 'Save files' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateMission)?.body.updateMission).toEqual({ id: 'mis_showcase', files: ['docs/sketch.md', 'docs/copy.md'] }))
    expect((await within(mission).findByText('docs/copy.md')).tagName).toBe('LI')

    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    fireEvent.click(within(review).getByRole('button', { name: 'Edit files' }))
    fireEvent.change(within(review).getByRole('textbox', { name: 'Files' }), { target: { value: 'rubrics/review.md' } })
    fireEvent.click(within(review).getByRole('button', { name: 'Save files' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toEqual({ id: 'gate_review', files: ['rubrics/review.md'] }))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toEqual({ id: 'gate_review', files: [] }))
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({ id: 'mis_showcase', files: [] }))
  })

  it('edits a formation brief in its window with undo', async () => {
    await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    fireEvent.click(within(frame).getByRole('button', { name: 'Edit brief' }))
    fireEvent.change(within(frame).getByRole('textbox', { name: 'Brief' }), { target: { value: 'Map the territory.\n\n- Known facts\n- Unknowns' } })
    fireEvent.click(within(frame).getByRole('button', { name: 'Save brief' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setBrief)?.body.setBrief).toEqual({ formationId: 'fmn_frame', goal: 'Map the territory.\n\n- Known facts\n- Unknowns', beadId: '', files: [], links: [] })
    })
    expect((await within(frame).findByText('Unknowns')).tagName).toBe('LI')

    fireEvent.click(within(frame).getByRole('button', { name: 'Edit files' }))
    fireEvent.change(within(frame).getByRole('textbox', { name: 'Files' }), { target: { value: 'docs/a.md, docs/b.md' } })
    fireEvent.click(within(frame).getByRole('button', { name: 'Save files' }))
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.setBrief).slice(-1)[0]?.body.setBrief).toEqual({ formationId: 'fmn_frame', goal: 'Map the territory.\n\n- Known facts\n- Unknowns', beadId: '', files: ['docs/a.md', 'docs/b.md'], links: [] })
    })

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.setBrief).slice(-1)[0]?.body.setBrief).toMatchObject({ files: [] }))
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.clearBrief).slice(-1)[0]?.body.clearBrief).toEqual({ formationId: 'fmn_frame' }))
  })

  it('states staffing in words and restaffs a slot from its window with undo', async () => {
    await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    const staffing = within(frame).getByRole('region', { name: 'Staffing' })
    expect(await within(staffing).findByText('Lead (controller) is Mason (mason) on codex, model mason-model, medium effort.')).toBeInTheDocument()
    expect(within(staffing).getByText('Worker is not staffed.')).toBeInTheDocument()

    fireEvent.change(within(staffing).getByRole('combobox', { name: 'Persona for Lead' }), { target: { value: 'hazel' } })
    await waitFor(() => {
      expect(patches.find(patch => patch.body.assignSlot)?.body.assignSlot).toEqual({ formationId: 'fmn_frame', slotId: 'slot_lead', agentId: 'hazel', harness: 'claude' })
    })
    expect(await within(staffing).findByText('Lead (controller) is Hazel (hazel) on claude, model hazel-model, medium effort.')).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.assignSlot).slice(-1)[0]?.body.assignSlot).toEqual({ formationId: 'fmn_frame', slotId: 'slot_lead', agentId: 'mason', harness: 'codex' })
    })
  })

  it('changes a formation type from its header chip, undoes it exactly, and survives reload', async () => {
    const { unmount } = await renderCockpit()
    const chip = screen.getByTestId('formation-type-fmn_frame')
    expect(chip).toHaveTextContent('orchestrated')
    fireEvent.click(chip)
    const menu = await screen.findByRole('menu', { name: 'Formation type' })
    expect(within(menu).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Solo', 'Peer'])
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Peer' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.setFormationType)?.body.setFormationType).toEqual({ id: 'fmn_frame', type: 'peer' })
    })
    await waitFor(() => expect(screen.getByTestId('formation-type-fmn_frame')).toHaveTextContent('peer'))
    expect(screen.getByTestId('formation-node-fmn_frame')).toHaveClass('type-peer')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.setFormationType).slice(-1)[0]?.body.setFormationType).toEqual({
        id: 'fmn_frame',
        type: 'orchestrated',
        slots: formation.slots,
      })
    })
    await waitFor(() => expect(screen.getByTestId('formation-type-fmn_frame')).toHaveTextContent('orchestrated'))

    fireEvent.click(screen.getByTestId('formation-type-fmn_frame'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Solo' }))
    await waitFor(() => expect(screen.getByTestId('formation-type-fmn_frame')).toHaveTextContent('solo'))

    unmount()
    const { container } = await renderCockpit()
    expect(screen.getByTestId('formation-type-fmn_frame')).toHaveTextContent('solo')
    expect(screen.getByTestId('formation-node-fmn_frame')).toHaveClass('type-solo')

    fireEvent.contextMenu(container.querySelector('.viewport') as HTMLElement, { clientX: 300, clientY: 300 })
    const create = await screen.findByRole('menu', { name: 'New' })
    expect(within(create).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Mission', 'Solo formation', 'Peer formation', 'Orchestrated formation', 'Gate'])
  })

  it('shows every slot of a retired formation type and converts it from the type chip', async () => {
    const legacyBoard = makeBoard()
    legacyBoard.formations = [{
      ...formation,
      type: 'flow',
      verification: undefined,
      slots: [
        { id: 'slot_plan', label: 'Plan', controller: false, agentId: 'mason', harness: 'codex' },
        { id: 'slot_execute', label: 'Execute', controller: false },
        { id: 'slot_push', label: 'Push', controller: false },
      ],
    }, judgeFormation] as TestBoard['formations']
    patches = installFetchMock({
      boards: [legacyBoard],
      validation: { errors: [{ code: 'invalid_formation_type', nodeId: 'fmn_frame', message: 'formation "fmn_frame" has unsupported type "flow"; change it to solo, peer or orchestrated with formation set-type, or delete it' }], warnings: [] },
    })
    await renderCockpit()
    for (const slot of ['slot_plan', 'slot_execute', 'slot_push']) {
      expect(screen.getByTestId(`slot-fmn_frame-${slot}`)).toBeInTheDocument()
    }
    expect(await screen.findByTestId('draft-marker-fmn_frame')).toHaveAttribute('title', expect.stringContaining('formation set-type'))
    fireEvent.click(screen.getByTestId('formation-type-fmn_frame'))
    const menu = await screen.findByRole('menu', { name: 'Formation type' })
    expect(within(menu).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Solo', 'Peer', 'Orchestrated'])
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Orchestrated' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setFormationType)?.body.setFormationType).toEqual({ id: 'fmn_frame', type: 'orchestrated' })
    })
    await waitFor(() => expect(screen.getByTestId('formation-type-fmn_frame')).toHaveTextContent('orchestrated'))
  })

  it('offers one solo choice per staffed slot so no agent is dropped silently', async () => {
    const staffedBoard = makeBoard()
    staffedBoard.formations = [{
      ...formation,
      slots: [
        { id: 'slot_lead', label: 'Lead', controller: true, agentId: 'mason', harness: 'codex' },
        { id: 'slot_worker', label: 'Worker', controller: false, agentId: 'hazel', harness: 'claude' },
      ],
    }, judgeFormation] as TestBoard['formations']
    patches = installFetchMock({ boards: [staffedBoard] })
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_frame'), { clientX: 420, clientY: 120 })
    const menu = await screen.findByRole('menu', { name: 'Formation actions' })
    expect(within(menu).queryByRole('menuitem', { name: 'Solo' })).toBeNull()
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Solo, keeping Worker (hazel)' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setFormationType)?.body.setFormationType).toEqual({ id: 'fmn_frame', type: 'solo', keepSlotId: 'slot_worker' })
    })
    await waitFor(() => expect(screen.getByTestId('slot-fmn_frame-slot_worker')).toBeInTheDocument())
    expect(screen.queryByTestId('slot-fmn_frame-slot_lead')).toBeNull()
  })

  it('dismisses context menus on Escape and outside pointerdown', async () => {
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    const menu = await screen.findByRole('menu', { name: 'New' })
    expect(menu.parentElement).toBe(document.body)
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu', { name: 'New' })).toBeNull())

    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    await screen.findByRole('menu', { name: 'New' })
    fireEvent.pointerDown(document.body)
    await waitFor(() => expect(screen.queryByRole('menu', { name: 'New' })).toBeNull())
  })

  it('unassigns a staffed slot from its context menu', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('slot-fmn_frame-slot_lead'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Unassign mason' }))
    await waitFor(() => {
      const assignment = patches.map(patch => patch.body.assignSlot as { slotId?: string; agentId?: string } | undefined).find(Boolean)
      expect(assignment).toEqual(expect.objectContaining({ slotId: 'slot_lead', agentId: '' }))
    })
  })

  it('saves nothing when a staffed slot is clicked or dropped back on itself', async () => {
    await renderCockpit()
    const lead = screen.getByTestId('slot-fmn_frame-slot_lead')
    const worker = screen.getByTestId('slot-fmn_frame-slot_worker')
    let target: HTMLElement = lead
    Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: () => target })
    try {
      fireEvent.pointerDown(lead, { button: 0, pointerId: 7, clientX: 400, clientY: 200 })
      fireEvent.pointerUp(window, { pointerId: 7, clientX: 401, clientY: 201 })
      fireEvent.pointerDown(lead, { button: 0, pointerId: 8, clientX: 400, clientY: 200 })
      fireEvent.pointerMove(window, { pointerId: 8, clientX: 460, clientY: 260 })
      fireEvent.pointerUp(window, { pointerId: 8, clientX: 400, clientY: 200 })
      fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
      await new Promise(resolve => setTimeout(resolve, 0))
      expect(patches.filter(patch => patch.body.assignSlot)).toEqual([])

      target = worker
      fireEvent.pointerDown(lead, { button: 0, pointerId: 9, clientX: 400, clientY: 200 })
      fireEvent.pointerMove(window, { pointerId: 9, clientX: 460, clientY: 260 })
      fireEvent.pointerUp(window, { pointerId: 9, clientX: 460, clientY: 260 })
      await waitFor(() => {
        expect(patches.map(patch => patch.body.assignSlot).filter(Boolean)).toEqual([
          { formationId: 'fmn_frame', slotId: 'slot_worker', agentId: 'mason', harness: 'codex' },
        ])
      })
    } finally {
      delete (document as { elementFromPoint?: unknown }).elementFromPoint
    }
  })

  it('adds an input port from the formation context menu', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_frame'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Add input port' }))
    await waitFor(() => {
      const port = patches.map(patch => patch.body.addPort as { direction?: string } | undefined).find(Boolean)
      expect(port).toEqual(expect.objectContaining({ direction: 'in' }))
    })
  })

  it('shows legacy inline verification as read-only migration input', async () => {
    await renderCockpit()
    const band = screen.getByRole('button', { name: 'Inspect legacy verification for Frame' })
    expect(band).toBe(screen.getByTestId('verify-band-fmn_frame'))
    band.focus()
    fireEvent.click(band)
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
		expect(dialog).toHaveAttribute('aria-modal', 'true')
		expect(dialog).toHaveAccessibleDescription(/Inline verification is retired/)
		expect(screen.getByRole('button', { name: 'Close legacy verification' })).toHaveFocus()
		expect(dialog).toHaveTextContent('Tests pass')
		expect(dialog).toHaveTextContent('Create and wire an explicit Gate')
		expect(screen.queryByRole('button', { name: 'Save verification' })).toBeNull()
		expect(screen.getByLabelText('Replacement Gate')).toHaveValue('')
		expect(screen.getByRole('button', { name: 'Remove legacy verification' })).toBeDisabled()
		fireEvent.change(screen.getByLabelText('Replacement Gate'), { target: { value: 'gate_review' } })
		fireEvent.click(screen.getByRole('button', { name: 'Remove legacy verification' }))
    await waitFor(() => {
      const removal = patches.map(patch => patch.body.removeVerification as { formationId?: string; replacementGateId?: string } | undefined).find(Boolean)
      expect(removal).toEqual({ formationId: 'fmn_frame', replacementGateId: 'gate_review' })
    })
    expect(patches.some(patch => patch.body.setVerification)).toBe(false)
    await waitFor(() => expect(dialog).not.toBeInTheDocument())
    expect(band).toHaveFocus()
  })

  it('restores the migration trigger after button and Escape dismissal', async () => {
    await renderCockpit()
    const band = screen.getByRole('button', { name: 'Inspect legacy verification for Frame' })

    band.focus()
    fireEvent.click(band)
    const close = await screen.findByRole('button', { name: 'Close legacy verification' })
    close.focus()
    fireEvent.click(close)
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Legacy verification · Frame' })).toBeNull())
    expect(band).toHaveFocus()

    fireEvent.click(band)
    await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Legacy verification · Frame' })).toBeNull())
    expect(band).toHaveFocus()
  })

  it('does not offer new inline verification authoring', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_judge'))
    const menu = await screen.findByRole('menu', { name: 'Formation actions' })
    expect(menu).not.toHaveTextContent('Add verification')
  })

  it('routes legacy removal through the migration dialog and keeps it open on failure', async () => {
    patches = installFetchMock({ removalFailure: true })
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_frame'))
    const menu = await screen.findByRole('menu', { name: 'Formation actions' })
    expect(menu).not.toHaveTextContent('Remove legacy verification')
		fireEvent.click(screen.getByRole('menuitem', { name: 'Migrate legacy verification' }))
		const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
		fireEvent.change(screen.getByLabelText('Replacement Gate'), { target: { value: 'gate_review' } })
		fireEvent.click(screen.getByRole('button', { name: 'Remove legacy verification' }))
    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Legacy verification migration failed')
    const localError = await within(dialog).findByRole('alert')
    expect(localError).toHaveTextContent('Could not remove legacy verification')
    expect(localError).not.toHaveTextContent('Legacy verification migration failed')
    expect(dialog).toBeInTheDocument()
  })

  it('locks Gate selection and duplicate removal while migration is pending', async () => {
    let releaseRemoval: (() => void) | undefined
    const removalGate = new Promise<void>(resolve => { releaseRemoval = resolve })
    patches = installFetchMock({ removalGate })
    await renderCockpit()
    const band = screen.getByRole('button', { name: 'Inspect legacy verification for Frame' })
    band.focus()
    fireEvent.click(band)
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    const replacement = within(dialog).getByLabelText('Replacement Gate')
    const remove = within(dialog).getByRole('button', { name: 'Remove legacy verification' })
    fireEvent.change(replacement, { target: { value: 'gate_review' } })
    fireEvent.click(remove)

    expect(await within(dialog).findByRole('status')).toHaveTextContent('Removing legacy verification')
    expect(replacement).toBeDisabled()
    expect(remove).toBeDisabled()
    fireEvent.click(remove)
    expect(patches.filter(patch => patch.body.removeVerification)).toHaveLength(1)

    await act(async () => releaseRemoval?.())
    await waitFor(() => expect(dialog).not.toBeInTheDocument())
    expect(band).toHaveFocus()
  })

  it('renders partial legacy verification without inventing missing evidence', async () => {
    const partialBoard = makeBoard()
    partialBoard.formations = [
      { ...formation, verification: { id: 'ver_partial' } as typeof formation.verification },
      judgeFormation,
    ]
    patches = installFetchMock({ boards: [partialBoard] })
    await renderCockpit()
    const band = screen.getByTestId('verify-band-fmn_frame')
    expect(band).toHaveTextContent('checks not recorded')
    fireEvent.click(band)
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    expect(dialog).toHaveTextContent('No criterion recorded')
		expect(dialog).toHaveTextContent('No failure policy recorded')
	})

	it('requires an explicit choice when multiple replacement Gates are wired', async () => {
		const multiGateBoard = makeBoard()
		multiGateBoard.gates = [
			...multiGateBoard.gates,
			{ id: 'gate_backup', title: 'Backup review', kinds: ['human'], criterion: 'Review again' },
		]
		multiGateBoard.connections = [
			...multiGateBoard.connections,
			{ id: 'edge_frame_backup', from: 'fmn_frame:port_frame_out', to: 'gate_backup:in' },
		]
		patches = installFetchMock({ boards: [multiGateBoard] })
		await renderCockpit()
		fireEvent.click(screen.getByTestId('verify-band-fmn_frame'))
		await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
		const replacement = screen.getByLabelText('Replacement Gate')
		expect(replacement).toHaveValue('')
		expect(screen.getByRole('button', { name: 'Remove legacy verification' })).toBeDisabled()
		fireEvent.change(replacement, { target: { value: 'gate_backup' } })
		fireEvent.click(screen.getByRole('button', { name: 'Remove legacy verification' }))
		await waitFor(() => {
			const removal = patches.map(patch => patch.body.removeVerification as { replacementGateId?: string } | undefined).find(Boolean)
			expect(removal).toEqual(expect.objectContaining({ replacementGateId: 'gate_backup' }))
		})
	})

	it('requires an explicitly wired replacement Gate before removal', async () => {
    const unwiredBoard = makeBoard()
    unwiredBoard.gates = []
    unwiredBoard.connections = unwiredBoard.connections.filter(connection => !connection.to.startsWith('gate_review:') && !connection.from.startsWith('gate_review:'))
    patches = installFetchMock({ boards: [unwiredBoard] })
    await renderCockpit()
    fireEvent.click(screen.getByTestId('verify-band-fmn_frame'))
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    expect(dialog).toHaveTextContent('Wire an explicit Gate from a Formation output before removal')
    expect(screen.getByRole('button', { name: 'Remove legacy verification' })).toBeDisabled()
    expect(patches.filter(patch => patch.body.removeVerification)).toEqual([])
  })

  it('closes a legacy migration dialog when the selected board changes', async () => {
    const secondBoard = { ...makeBoard(), id: 'brd_second', slug: 'second-board', title: 'Second board', etag: 'second-etag' }
    patches = installFetchMock({ boards: [makeBoard(), secondBoard] })
    await renderCockpit()
    const band = screen.getByTestId('verify-band-fmn_frame')
    band.focus()
    fireEvent.click(band)
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    within(dialog).getByLabelText('Replacement Gate').focus()
    fireEvent.change(screen.getByTestId('board-picker'), { target: { value: 'second-board' } })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Legacy verification · Frame' })).toBeNull())
    expect(screen.getByTestId('verify-band-fmn_frame')).toHaveFocus()
    expect(patches.filter(patch => patch.body.removeVerification)).toEqual([])
  })

  it('labels historical verification verdicts as non-authorizing evidence', async () => {
    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    patches = installFetchMock({
      runEvents: [
        { runId: 'run_legacy', seq: 1, type: 'node_output', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 2, type: 'verification_verdict', nodeId: 'fmn_frame', verdict: 'fail' },
      ],
    })
    await renderCockpit()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/formations/runs/run_legacy/events', expect.anything()))
    fireEvent.click(screen.getByTestId('verify-band-fmn_frame'))
    const dialog = await screen.findByRole('dialog', { name: 'Legacy verification · Frame' })
    expect(dialog).toHaveTextContent('Legacy verification evidence · non-authorizing')
    expect(dialog).toHaveTextContent('seq 2 · fail')
  })

  it('loads final evidence when a lab mission completes before its start response is read', async () => {
    patches = installFetchMock({
      runStatus: { status: 'succeeded', final: true },
      runEvents: [{runId:'run_legacy',seq:1,type:'node_output',nodeId:'fmn_frame',status:'done'}],
    })
    await renderCockpit()
    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const dialog = await screen.findByRole('dialog', { name: 'Start mission' })
    fireEvent.change(within(dialog).getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Implement the requested change' } })
    fireEvent.change(within(dialog).getByLabelText('Bead'), { target: { value: 'form-proof' } })
    fireEvent.change(within(dialog).getByLabelText('Maximum dispatches'), { target: { value: '8' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start mission' }))
    await screen.findByTestId('inspect-node-fmn_frame')
    expect(fetch).toHaveBeenCalledWith('/api/formations/runs', expect.objectContaining({ body: expect.stringContaining('"cwd":"/work/project"') }))
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => url === '/api/formations/runs' && init?.method === 'POST')
    expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ cwd: '/work/project', brief: 'Implement the requested change', beadId: 'form-proof', limits: { maxDispatch: 8 } })
    expect(localStorage.getItem('chrote-formations-active-run-test-board')).toBeNull()
  })

  it('fits the canvas to its cards and keeps the zoom level numeric beside a formation-kind gate', async () => {
    const board = makeBoard()
    patches = installFetchMock({ boards: [{ ...board, gates: [{ ...gate, kinds: ['formation'] }] }] })
    await renderCockpit()
    const world = screen.getByTestId('formations-world')
    const scaleOf = () => Number(/scale\(([^)]*)\)/.exec(world.style.transform)?.[1])
    await waitFor(() => expect(Number.isFinite(scaleOf())).toBe(true))
    await waitFor(() => expect(document.querySelector('.zoomlevel')).toHaveTextContent(`${Math.round(scaleOf() * 100)}%`))
    fireEvent.click(screen.getByRole('button', { name: 'FIT' }))
    await waitFor(() => expect(document.querySelector('.zoomlevel')).toHaveTextContent(/^\d+%$/))
  })

  it('keeps gate kind chips out of the canvas card classes', async () => {
    const board = makeBoard()
    patches = installFetchMock({ boards: [{ ...board, gates: [{ ...gate, kinds: ['formation', 'human'] }] }] })
    await renderCockpit()
    const chips = within(screen.getByTestId('gate-kinds-gate_review')).getAllByText(/judge|human/)
    expect(chips.map(chip => chip.className)).toEqual(['gkind gkind-formation', 'gkind gkind-human'])
    const gateCard = screen.getByTestId('gate-node-gate_review')
    expect(gateCard.querySelector('.gs')).toHaveTextContent(/^Review the frame$/)
    expect(gateCard.textContent).not.toMatch(/formation/)
    expect(screen.getByTestId('formations-world').querySelectorAll('.formation')).toHaveLength(board.formations.length)
  })

  it('says in each formation card what the selected run did with the node', async () => {
    patches = installFetchMock()
    await renderCockpit()
    expect(screen.getByTestId('output-status-fmn_frame')).toHaveTextContent('no output yet')
    cleanup()

    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    patches = installFetchMock({
      runStatus: { status: 'succeeded', final: true },
      runEvents: [
        { runId: 'run_legacy', seq: 1, type: 'node_started', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 2, type: 'node_output', nodeId: 'fmn_frame', status: 'done' },
      ],
    })
    await renderCockpit()
    await waitFor(() => expect(screen.getByTestId('output-status-fmn_frame')).toHaveTextContent('output ready'))
    expect(screen.getByTestId('output-status-fmn_frame')).toHaveClass('done')
    expect(screen.getByTestId('output-status-fmn_judge')).toHaveTextContent('not reached')
    expect(screen.queryByText('no output yet')).toBeNull()
  })

  it('shows the node status with its output and slot evidence from the evidence routes', async () => {
    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    patches = installFetchMock({
      runEvents: [
        { runId: 'run_legacy', seq: 1, type: 'node_started', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 2, type: 'slot_dispatch', nodeId: 'fmn_frame', slotId: 'slot_lead' },
        { runId: 'run_legacy', seq: 3, type: 'node_output', nodeId: 'fmn_frame', status: 'done' },
      ],
    })
    const projectionFetch = globalThis.fetch
    const evidence = {
      runId: 'run_legacy', nodeId: 'fmn_frame', kind: 'formation',
      attempts: [{
        attempt: 1, startedSeq: 1, inputs: [],
        dispatches: [{ seq: 2, slotId: 'slot_lead', agentId: 'mason', harness: 'openai-codex', brief: false, status: 'ok' }],
        output: { seq: 3, status: 'done', text: { text: 'Framed the **problem**', bytes: 22 }, ports: [] },
      }],
    }
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const data = url === '/api/formations/runs/run_legacy/evidence/nodes/fmn_frame' ? { evidence }
        : url === '/api/formations/runs/run_legacy/evidence/artifacts' ? { artifacts: [], truncated: false } : null
      if (!data) return projectionFetch(input, init)
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data }) } as unknown as Response)
    }) as typeof fetch
    await renderCockpit()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/formations/runs/run_legacy/events', expect.anything()))
    fireEvent.click(await screen.findByTestId('inspect-node-fmn_frame'))
    const dialog = await screen.findByTestId('node-inspector')
    expect(within(dialog).getByTestId('node-evidence-state')).toHaveTextContent('done')
    expect(await within(dialog).findByTestId('node-output-value')).toHaveTextContent('Framed the problem')
    expect(dialog).toHaveTextContent('slot_lead')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close run evidence' }))
    await waitFor(() => expect(screen.queryByTestId('node-inspector')).toBeNull())
  })

  it('surfaces open escalations as a needs-you banner and marks the escalated node', async () => {
    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    patches = installFetchMock({
      runStatus: { status: 'blocked', final: false, resumeAllowed: true },
      runEvents: [
        { runId: 'run_legacy', seq: 1, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
      escalations: [
        { runId: 'run_legacy', seq: 5, gateId: 'gate_review', nodeId: 'gate_review', severity: 'stop', reason: 'operator taste needed', source: 'agent', trigger: 'sentinel', blocks: true },
      ],
    })
    await renderCockpit()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/formations/runs/run_legacy/escalations', expect.anything()))

    const banner = await screen.findByTestId('escalations-banner')
    expect(banner).toHaveTextContent('Needs you')
    const item = within(banner).getByTestId('escalation-5')
    expect(item).toHaveTextContent('operator taste needed')
    expect(item).toHaveTextContent('gate_review')
    expect(item).toHaveTextContent('stop')
    await waitFor(() => expect(screen.getByTestId('gate-node-gate_review')).toHaveClass('needs-you'))

    fireEvent.click(item)
    expect(await screen.findByTestId('node-inspector')).toHaveTextContent('Run evidence · Review')
  })

  it('shows the run state of a node in its window with a way into its evidence', async () => {
    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    patches = installFetchMock({
      runStatus: { status: 'running', final: false },
      runEvents: [{ runId: 'run_legacy', seq: 1, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' }],
    })
    await renderCockpit()
    await waitFor(() => expect(screen.getByTestId('inspect-node-gate_review')).toBeInTheDocument())
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    const run = within(review).getByRole('region', { name: 'Run' })
    expect(run).toHaveTextContent('Running')
    const open = within(run).getByRole('button', { name: 'Open run evidence' })
    open.focus()
    fireEvent.click(open)
    expect(await screen.findByTestId('node-inspector')).toHaveTextContent('Run evidence · Review')
    // Escape closes the evidence above the window before the window itself.
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByTestId('node-inspector')).toBeNull())
    expect(screen.getByRole('dialog', { name: 'Gate · Review' })).toBe(review)

    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Mission · Showcase')
    expect(within(mission).queryByRole('region', { name: 'Run' })).toBeNull()
  })

  type ListedRun = { runId: string; status: string; final: boolean; boardSlug: string; missionId: string; eventCount: number; waitingGates?: Array<{ gateId: string; requestedSeq: number }> }
  function installRunsMock(runs: ListedRun[], events: Record<string, TestRunEvent[]> = {}) {
    const verdicts: Array<{ url: string; body: Record<string, unknown> }> = []
    const base = globalThis.fetch
    ;(globalThis as Record<string, unknown>).fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const reply = (data: unknown, status = 200) => Promise.resolve({
        ok: status < 300,
        status,
        headers: { get: () => null },
        json: () => Promise.resolve(status < 300 ? { success: true, data } : { success: false, error: { code: 'NOT_FOUND', message: 'Not Found' } }),
        text: () => Promise.resolve(''),
      })
      const listed = url.match(/^\/api\/formations\/runs\?board=([^&]+)$/)
      if (listed) return reply(runs.filter(run => run.boardSlug === decodeURIComponent(listed[1])))
      const runURL = url.match(/^\/api\/formations\/runs\/([^/?]+)(\/.*)?$/)
      if (runURL) {
        const run = runs.find(item => item.runId === runURL[1])
        if (!run) return reply(null, 404)
        if (!runURL[2]) return reply({ status: run })
        if (runURL[2] === '/events') return reply({ events: events[run.runId] || [] })
        if (runURL[2] === '/escalations') return reply({ escalations: [] })
        if (runURL[2].endsWith('/request')) return reply({ request: { gateId: 'gate_review', requestedSeq: 4, criterion: 'Review the frame', input: { fromNodeId: 'fmn_frame', text: 'Question for ' + run.runId, truncated: false } } })
        if (runURL[2].endsWith('/verdict')) {
          verdicts.push({ url, body: JSON.parse(String(init?.body)) })
          return reply({ runId: run.runId })
        }
      }
      return base(input, init)
    }) as unknown as typeof fetch
    return verdicts
  }
  const waitingEvents = (runId: string): TestRunEvent[] => [{ runId, seq: 4, type: 'human_input_requested', nodeId: 'gate_review', gateId: 'gate_review' }]

  it('finds a run started outside this browser and answers its gate', async () => {
    const runs = [{ runId: 'run_01CLI', status: 'waiting_human', final: false, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] }]
    const verdicts = installRunsMock(runs, { run_01CLI: waitingEvents('run_01CLI') })
    expect(localStorage.length).toBe(0)
    await renderCockpit()

    expect(await screen.findByTestId('run-banner')).toHaveTextContent('waiting_human')
    const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    await waitFor(() => expect(within(panel).getByText('Question for run_01CLI')).toBeInTheDocument())
    expect(fetch).toHaveBeenCalledWith('/api/formations/runs?board=test-board', expect.anything())
    expect(screen.queryByRole('combobox', { name: 'Choose run' })).toBeNull()

    fireEvent.change(within(panel).getByLabelText('Your response'), { target: { value: 'Postgres' } })
    await act(async () => { fireEvent.click(within(panel).getByRole('button', { name: 'Approve' })) })
    await waitFor(() => expect(verdicts).toEqual([{ url: '/api/formations/runs/run_01CLI/gates/gate_review/verdict', body: { actor: 'agent:ui', verdict: 'pass', requestedSeq: 4, reason: 'Postgres' } }]))
  })

  it('shows the open run that needs the operator and offers a run picker', async () => {
    installRunsMock([
      { runId: 'run_01A', status: 'waiting_human', final: false, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
      { runId: 'run_01B', status: 'running', final: false, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 2 },
      { runId: 'run_01C', status: 'succeeded', final: true, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 9 },
    ], { run_01A: waitingEvents('run_01A') })
    await renderCockpit()

    const picker = await screen.findByRole('combobox', { name: 'Choose run' })
    expect(picker).toHaveValue('run_01A')
    expect(within(picker).getAllByRole('option').map(option => option.getAttribute('value'))).toEqual(['run_01A', 'run_01B'])
    expect(await screen.findByRole('dialog', { name: 'Answer gate Review' })).toBeInTheDocument()

    fireEvent.change(picker, { target: { value: 'run_01B' } })
    await waitFor(() => expect(screen.getByTestId('run-banner')).toHaveTextContent('running'))
    expect(screen.queryByRole('dialog', { name: 'Answer gate Review' })).toBeNull()
    expect(window.location.search).toBe('?board=test-board&run=run_01B')
  })

  it('opens a board and run from a link and keeps them on reload', async () => {
    window.history.replaceState(null, '', '/?board=second-board&run=run_01LINK')
    const second = { ...makeBoard(), id: 'brd_second', slug: 'second-board', title: 'Second board', etag: 'second-etag' }
    patches = installFetchMock({ boards: [makeBoard(), second] })
    installRunsMock([
      { runId: 'run_01LINK', status: 'waiting_human', final: false, boardSlug: 'second-board', missionId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
      { runId: 'run_01NEWER', status: 'waiting_human', final: false, boardSlug: 'second-board', missionId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
    ], { run_01LINK: waitingEvents('run_01LINK'), run_01NEWER: waitingEvents('run_01NEWER') })

    for (let load = 0; load < 2; load++) {
      const { unmount } = await renderCockpit()
      await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveValue('second-board'))
      expect(await screen.findByRole('combobox', { name: 'Choose run' })).toHaveValue('run_01LINK')
      const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
      await waitFor(() => expect(within(panel).getByText('Question for run_01LINK')).toBeInTheDocument())
      expect(window.location.search).toBe('?board=second-board&run=run_01LINK')
      unmount()
    }
  })

  it('says when a linked run or board does not exist', async () => {
    window.history.replaceState(null, '', '/?board=test-board&run=run_01GONE')
    installRunsMock([{ runId: 'run_01OPEN', status: 'running', final: false, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 2 }])
    const { unmount } = await renderCockpit()
    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Run run_01GONE from the link was not found')
    await waitFor(() => expect(screen.getByTestId('run-banner')).toHaveTextContent('running'))
    expect(window.location.search).toBe('?board=test-board')
    unmount()

    window.history.replaceState(null, '', '/?board=no-such-board&run=run_01OPEN')
    await renderCockpit()
    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Board "no-such-board" from the link was not found')
    expect(screen.getByTestId('board-picker')).toHaveValue('test-board')
  })

  it('names where a blocked run stopped, rings that gate and locates it from the run bar', async () => {
    installRunsMock([{ runId: 'run_01BLOCK', status: 'blocked', final: false, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 3 }], {
      run_01BLOCK: [
        { runId: 'run_01BLOCK', seq: 1, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01BLOCK', seq: 2, type: 'judge_attempt_failed', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01BLOCK', seq: 3, type: 'run_blocked', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    const projection = globalThis.fetch
    const evidence = { runId: 'run_01BLOCK', nodeId: 'gate_review', kind: 'gate', problems: [
      { seq: 3, type: 'run_blocked', reason: { text: 'invalid judge result: missing verdict block', bytes: 42 }, resumeAllowed: false },
    ] }
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => String(input) === '/api/formations/runs/run_01BLOCK/evidence/nodes/gate_review'
      ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data: { evidence } }) } as unknown as Response)
      : projection(input, init)) as typeof fetch
    await renderCockpit()

    const point = await screen.findByTestId('run-point')
    await waitFor(() => expect(point).toHaveTextContent('blocked at Review: invalid judge result: missing verdict block'))
    const gate = screen.getByTestId('gate-node-gate_review')
    expect(gate).toHaveClass('blocked')
    expect(within(gate).getByTestId('run-chip-gate_review')).toHaveTextContent('blocked')
    fireEvent.click(point)
    expect(gate).toHaveClass('located')
  })

  it('shows what a finished run produced and opens it in a file window', async () => {
    window.history.replaceState(null, '', '/?board=test-board&run=run_01DONE')
    installRunsMock([{ runId: 'run_01DONE', status: 'succeeded', final: true, boardSlug: 'test-board', missionId: 'mis_showcase', eventCount: 6 }], {
      run_01DONE: [
        { runId: 'run_01DONE', seq: 1, type: 'node_started', nodeId: 'fmn_frame', attempt: 1 },
        { runId: 'run_01DONE', seq: 2, type: 'node_output', nodeId: 'fmn_frame', status: 'done' },
        { runId: 'run_01DONE', seq: 3, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01DONE', seq: 4, type: 'node_output', nodeId: 'fmn_judge', status: 'done' },
        { runId: 'run_01DONE', seq: 5, type: 'gate_verdict', nodeId: 'gate_review', gateId: 'gate_review', verdict: 'pass' },
        { runId: 'run_01DONE', seq: 6, type: 'run_succeeded' },
      ],
    })
    const projection = globalThis.fetch
    const text = (value: string) => ({ text: value, bytes: value.length })
    const output = (nodeId: string, seq: number, port: string, body: string, artifact?: string) => ({ evidence: { runId: 'run_01DONE', nodeId, kind: 'formation', attempts: [
      { attempt: 1, inputs: [], dispatches: [], output: { seq, text: text(body), ports: [{ portId: port, text: text(body), ref: artifact ? { artifact } : undefined }] } },
    ] } })
    const evidence: Record<string, unknown> = {
      '/api/formations/runs/run_01DONE/evidence/nodes/fmn_frame': output('fmn_frame', 2, 'port_frame_out', '# Frame\n\nThe **problem**', 'frame.md'),
      '/api/formations/runs/run_01DONE/evidence/nodes/fmn_judge': output('fmn_judge', 4, 'port_judge_out', 'verdict pass'),
      '/api/formations/runs/run_01DONE/evidence/artifacts': { artifacts: [{ name: 'frame.md', size: 24, modifiedAt: '' }], truncated: false },
      '/api/formations/runs/run_01DONE/evidence/artifacts/frame.md': { artifact: { name: 'frame.md', size: 24, modifiedAt: '', kind: 'markdown', text: text('# Frame\n\nThe **problem**') } },
    }
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => String(input) in evidence
      ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data: evidence[String(input)] }) } as unknown as Response)
      : projection(input, init)) as typeof fetch
    await renderCockpit()

    const produced = await screen.findByTestId('run-produced')
    await waitFor(() => expect(within(produced).getAllByRole('button').map(button => button.textContent)).toEqual(['▤frame.md', '+1']))
    expect(within(screen.getByTestId('produced-fmn_frame')).getByRole('button', { name: 'frame.md' })).toBeInTheDocument()
    expect(within(screen.getByTestId('produced-fmn_judge')).getByRole('button', { name: 'Output' })).toBeInTheDocument()

    fireEvent.click(within(produced).getByRole('button', { name: 'frame.md' }))
    const file = await screen.findByRole('dialog', { name: 'file frame.md' })
    expect(await within(file).findByRole('heading', { name: 'Frame' })).toBeInTheDocument()
    expect(file).toHaveTextContent('Frame · frame.md')
  })

  it('answers a pending human gate from its upstream output', async () => {
    localStorage.setItem('chrote-formations-active-run-test-board', 'run_legacy')
    installFetchMock({
      runStatus: { status: 'waiting_human', final: false },
      runEvents: [
        { runId: 'run_legacy', seq: 3, type: 'node_output', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 4, type: 'human_input_requested', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    const questions = '1. Which database?\n2. Who signs off the brief?'
    const verdicts: Array<Record<string, unknown>> = []
    const coordinator = globalThis.fetch
    ;(globalThis as Record<string, unknown>).fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const reply = (data: unknown) => Promise.resolve({ ok: true, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data }), text: () => Promise.resolve('') })
      if (url === '/api/formations/runs/run_legacy/gates/gate_review/request') {
        return reply({ request: { gateId: 'gate_review', requestedSeq: 4, criterion: 'Review the frame', input: { fromNodeId: 'fmn_frame', fromPortId: 'port_frame_out', text: questions, truncated: false } } })
      }
      if (url === '/api/formations/runs/run_legacy/gates/gate_review/verdict') {
        verdicts.push(JSON.parse(String(init?.body)))
        return reply({ runId: 'run_legacy' })
      }
      return coordinator(input, init)
    }) as unknown as typeof fetch
    await renderCockpit()

    const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    expect(within(panel).getByText('Review the frame')).toBeInTheDocument()
    await waitFor(() => expect(within(panel).getByText('From Frame')).toBeInTheDocument())
    expect(within(panel).getByTestId('gate-answer-upstream').querySelector('pre')?.textContent).toBe(questions)
    expect(screen.queryByRole('button', { name: 'Approve gate gate_review' })).toBeNull()

    fireEvent.change(within(panel).getByLabelText('Your response'), { target: { value: '1. Postgres.\n2. The operator.' } })
    await act(async () => { fireEvent.click(within(panel).getByRole('button', { name: 'Approve' })) })
    await waitFor(() => expect(verdicts).toEqual([{ actor: 'agent:ui', verdict: 'pass', requestedSeq: 4, reason: '1. Postgres.\n2. The operator.' }]))
  })

  it('detaches the judge from the gate context menu', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('gate-node-gate_review'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Detach judge' }))
    await waitFor(() => {
      const detach = patches.map(patch => patch.body.detachGateJudge as { gateId?: string } | undefined).find(Boolean)
      expect(detach).toEqual(expect.objectContaining({ gateId: 'gate_review' }))
    })
  })

  it('leaves a human gate when detaching the judge from a judge-only gate, and undo restores kinds and chain', async () => {
    const judgeOnly = makeBoard()
    judgeOnly.gates = [{ ...gate, kinds: ['formation'] }]
    patches = installFetchMock({ boards: [judgeOnly] })
    await renderCockpit()
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent(/^judge$/)

    fireEvent.contextMenu(screen.getByTestId('gate-node-gate_review'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Detach judge' }))
    await waitFor(() => expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent(/^human$/))
    expect(screen.queryByTestId('formation-wire-edge_judge_send')).toBeNull()

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setGateJudge)?.body.setGateJudge).toEqual({ gateId: 'gate_review', chain: ['fmn_judge'] })
    })
    const restore = patches.findIndex(patch => patch.body.updateGate)
    expect(patches[restore]?.body.updateGate).toEqual({ id: 'gate_review', title: 'Review', kinds: ['formation'], criterion: 'Review the frame', check: '', checkVersion: '', checkValue: '' })
    expect(restore).toBeLessThan(patches.findIndex(patch => patch.body.setGateJudge))
    expect(patches.findIndex(patch => patch.body.detachGateJudge)).toBeLessThan(restore)
  })

  it('deletes a mission from its context menu', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('mission-node-mis_showcase'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete mission' }))
    await waitFor(() => {
      const removal = patches.map(patch => patch.body.deleteMission as { id?: string } | undefined).find(Boolean)
      expect(removal).toEqual(expect.objectContaining({ id: 'mis_showcase' }))
    })
  })

  it('uses the shared explicit Arrange operation for whole-board movement', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByTestId('arrange-layout'))
    await waitFor(() => {
      expect(patches).toContainEqual({
        url: '/api/formations/boards/test-board/layout',
        body: { arrange: true },
      })
    })
  })

  it('names what feeds each input and shows a brief once, under the title', async () => {
    const briefed = makeBoard()
    briefed.formations = [{ ...formation, brief: { goal: 'Frame the page' } } as typeof formation, judgeFormation]
    briefed.connections = [...briefed.connections, { id: 'edge_gate_retry', from: 'gate_review:fail', to: 'fmn_frame:port_frame_in' }]
    patches = installFetchMock({ boards: [briefed] })
    await renderCockpit()

    const frame = screen.getByTestId('formation-node-fmn_frame')
    const feed = frame.querySelector('.fio.in .io-text') as HTMLElement
    expect(feed).toHaveTextContent(/^from Showcase, Review \(fail\)$/)
    expect(feed).not.toHaveClass('placeholder')
    const summary = frame.querySelector('.tg') as HTMLElement
    expect(summary).toHaveTextContent(/^Frame the page$/)
    expect(summary).not.toHaveClass('placeholder')
    expect(frame.textContent?.split('Frame the page')).toHaveLength(2)

    const judge = screen.getByTestId('formation-node-fmn_judge')
    expect(judge.querySelector('.fio.in .io-text')).toHaveTextContent(/^from Review \(judge\)$/)
    expect(judge.querySelector('.tg')).toHaveClass('placeholder')
    expect(screen.getByTestId('gate-node-gate_review').querySelector('.gs')).not.toHaveClass('placeholder')
  })

  it('collapses the roster and remembers the width it was resized to', async () => {
    const { unmount } = await renderCockpit()
    let roster = screen.getByTestId('agent-roster')
    const handle = screen.getByRole('separator', { name: 'Resize agent roster' })
    expect(roster.style.getPropertyValue('--roster-width')).toBe('236px')

    fireEvent.pointerDown(handle, { button: 0, clientX: 236 })
    fireEvent.pointerMove(window, { clientX: 336 })
    expect(roster.style.getPropertyValue('--roster-width')).toBe('336px')
    fireEvent.pointerUp(window, { clientX: 336 })
    expect(localStorage.getItem('chrote-formations-roster-width')).toBe('336')
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    expect(handle).toHaveAttribute('aria-valuenow', '320')
    fireEvent.pointerDown(handle, { button: 0, clientX: 320 })
    fireEvent.pointerUp(window, { clientX: 2000 })
    expect(roster.style.getPropertyValue('--roster-width')).toBe('480px')

    fireEvent.click(screen.getByRole('button', { name: 'Collapse agent roster' }))
    expect(roster).toHaveClass('collapsed')
    expect(screen.getByRole('button', { name: 'Expand agent roster' })).toHaveAttribute('aria-expanded', 'false')
    expect(localStorage.getItem('chrote-formations-roster-collapsed')).toBe('true')

    unmount()
    await renderCockpit()
    roster = screen.getByTestId('agent-roster')
    expect(roster).toHaveClass('collapsed')
    expect(roster.style.getPropertyValue('--roster-width')).toBe('480px')
    fireEvent.click(screen.getByRole('button', { name: 'Expand agent roster' }))
    expect(roster).not.toHaveClass('collapsed')
    expect(localStorage.getItem('chrote-formations-roster-collapsed')).toBe('false')
  })

  it('closes a node window and the Tool dialog on Escape', async () => {
    patches = installFetchMock({ boards: [makeToolBoard()] })
    await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    fireEvent.keyDown(frame, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Formation · Frame' })).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Tool Normalize report' }))
    expect(await screen.findByRole('dialog', { name: 'Tool details: Normalize report' })).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Tool details: Normalize report' })).toBeNull())
  })

  it('cancels the active interaction when the cockpit unmounts', async () => {
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement

    fireEvent.pointerDown(viewport, { button: 0, pointerId: 5, clientX: 800, clientY: 600 })
    fireEvent.pointerMove(window, { pointerId: 5, clientX: 830, clientY: 620 })
    expect(viewport).toHaveClass('panning')

    unmount()
    expect(viewport).not.toHaveClass('panning')
  })
})
