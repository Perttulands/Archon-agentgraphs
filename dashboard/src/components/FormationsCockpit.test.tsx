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
    fireEvent.click(screen.getByRole('button', { name: 'Expand shared notepad' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Board note' })
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
    fireEvent.click(screen.getByRole('button', { name: 'Expand shared notepad' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Board note' })
    fireEvent.click(screen.getByRole('button', { name: 'Delete board' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete board' })
    fireEvent.change(boardNote, { target: { value: 'Draft made after delete opened' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive board' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Delete board' })).toBeNull())
    expect(boardNote).toHaveValue('Draft made after delete opened')
    expect(recordedMutations.some(mutation => mutation.method === 'DELETE')).toBe(false)
  })

  it('shows mixed-author note threads and replies without overwriting', async () => {
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
    expect(preview).toHaveTextContent('archon')
    expect(preview).toHaveTextContent('+1 earlier')
    expect(preview).toHaveTextContent('Staffed Mason as the lead.')
    expect(screen.getByTestId('formation-node-fmn_frame')).toHaveClass('has-note')

    const notepad = await screen.findByRole('complementary', { name: 'Shared board notepad' })
    fireEvent.click(within(screen.getByTestId('formation-node-fmn_frame')).getByRole('button', { name: 'Open notes for Frame' }))
    expect(within(notepad).getByLabelText('Element')).toHaveValue('fmn_frame')
    const thread = within(notepad).getByRole('list', { name: 'Element note thread' })
    expect(within(thread).getAllByRole('listitem').map(item => item.textContent)).toEqual([
      expect.stringContaining('Builder owns this element.'),
      expect.stringContaining('Staffed Mason as the lead.'),
    ])
    expect(screen.getByTestId('note-entry-nte_operator')).toHaveClass('human')
    expect(screen.getByTestId('note-entry-nte_agent')).toHaveClass('agent')
    expect(within(screen.getByTestId('note-entry-nte_operator')).getByText('operator')).toHaveClass('note-author', 'human')
    expect(within(screen.getByTestId('note-entry-nte_agent')).getByText('archon')).toHaveClass('note-author', 'agent')
    expect(within(screen.getByTestId('note-entry-nte_agent')).queryByRole('button')).toBeNull()

    const reply = within(notepad).getByLabelText('Element note')
    expect(reply).toHaveValue('')
    fireEvent.change(reply, { target: { value: 'Add a reviewer too.' } })
    fireEvent.click(within(notepad).getByRole('button', { name: 'Add element note' }))
    await waitFor(() => expect(within(thread).getAllByRole('listitem')).toHaveLength(3))
    expect(reply).toHaveValue('')
    const noteCall = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url).endsWith('/notes') && init?.method === 'PATCH')
    expect(JSON.parse(String(noteCall?.[1]?.body))).toEqual({ target: 'fmn_frame', action: 'append', text: 'Add a reviewer too.', author: 'human:ui' })

    fireEvent.click(within(screen.getByTestId('note-entry-nte_operator')).getByRole('button', { name: /^Edit your note/ }))
    expect(reply).toHaveValue('Builder owns this element.')
    fireEvent.change(reply, { target: { value: 'Builder and reviewer own this element.' } })
    fireEvent.click(within(notepad).getByRole('button', { name: 'Save edited element note' }))
    await waitFor(() => expect(screen.getByTestId('note-entry-nte_operator')).toHaveTextContent('Builder and reviewer own this element.'))
    expect(screen.getByTestId('note-entry-nte_operator')).toHaveTextContent('edited')
    expect(screen.getByTestId('note-entry-nte_agent')).toHaveTextContent('Staffed Mason as the lead.')

    const boardThread = within(notepad).getByRole('list', { name: 'Board note thread' })
    fireEvent.click(within(boardThread).getByRole('button', { name: /^Delete your note/ }))
    await waitFor(() => expect(within(notepad).queryByRole('list', { name: 'Board note thread' })).toBeNull())

    fireEvent.click(within(notepad).getByRole('button', { name: 'Collapse shared notepad' }))
    expect(screen.queryByLabelText('Board note')).toBeNull()
    expect(screen.getByRole('button', { name: 'Expand shared notepad' })).toBeInTheDocument()
  })

  it('adds a new element note from the sticky-note affordance', async () => {
    await renderCockpit()
    const judge = screen.getByTestId('formation-node-fmn_judge')
    expect(judge).not.toHaveClass('has-note')

    fireEvent.click(within(judge).getByRole('button', { name: 'Add note for Judge' }))
    const elementNote = screen.getByLabelText('Element note')
    fireEvent.change(elementNote, { target: { value: 'Use this as the release judge.' } })
    fireEvent.click(within(judge).getByRole('button', { name: 'Add note for Judge' }))
    expect(elementNote).toHaveValue('Use this as the release judge.')
    const addElementNote = screen.getByRole('button', { name: 'Add element note' })
    await waitFor(() => expect(addElementNote).toBeEnabled())
    fireEvent.click(addElementNote)

    await waitFor(() => expect(judge).toHaveClass('has-note'))
    expect(screen.getByRole('note', { name: 'Notes for Judge' })).toHaveTextContent('Use this as the release judge.')
  })

  it('preserves a local note draft and offers an explicit reload after a repeated conflict', async () => {
    patches = installFetchMock({ boardNotes: { board: [noteEntry('nte_server', 'agent:archon', 'Server version')] }, notePatchConflict: true })
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Expand shared notepad' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Board note' })
    fireEvent.change(boardNote, { target: { value: 'Local draft' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add board note' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Shared notes changed')
    expect(boardNote).toHaveValue('Local draft')
    expect(vi.mocked(fetch).mock.calls.filter(([url, init]) => String(url).endsWith('/notes') && init?.method === 'PATCH')).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Reload shared notes (discard local draft)' })).toBeEnabled()
  })

  it('protects unsaved notes from board switches, creation, and deletion', async () => {
    const second = { ...makeBoard(), id: 'board-2', slug: 'second-board', title: 'Second board', etag: 'board-2-etag' }
    patches = installFetchMock({ boards: [makeBoard(), second] })
    await renderCockpit()

    fireEvent.click(screen.getByRole('button', { name: 'Expand shared notepad' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Board note' })
    fireEvent.change(boardNote, { target: { value: 'Unsaved local context' } })
    fireEvent.change(screen.getByTestId('board-picker'), { target: { value: 'second-board' } })

    expect(screen.getByTestId('board-picker')).toHaveValue('test-board')
    expect(screen.getByRole('alert')).toHaveTextContent('Save the current notes before leaving this board')
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
    fireEvent.change(within(dialog).getByLabelText('Forbidden text'), { target: { value: 'error' } })
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

  it('creates a code Gate with no optional input and reloads it intact', async () => {
    patches = installFetchMock({ freshCreateLayout: true })
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Create gate' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create gate' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.createGate)?.body.createGate).toMatchObject({
        title: 'Review gate',
        kinds: ['code'],
        criterion: '',
        check: 'output_absent',
        checkVersion: '1',
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
    expect(within(dialog).getByLabelText('Code kind')).toBeChecked()
    expect(within(dialog).getByLabelText('Code kind')).toBeDisabled()

    fireEvent.click(within(dialog).getByLabelText('Human kind'))
    fireEvent.click(within(dialog).getByLabelText('Judge kind'))
    expect(within(dialog).getByText("Attach a judge formation from the gate's judge socket. A run needs one.")).toBeInTheDocument()
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

  it('converts a code gate to a human gate from Edit gate and undoes it', async () => {
    const codeBoard = makeBoard()
    codeBoard.gates = [{ ...gate, check: 'output_absent', checkVersion: '1', checkValue: 'complaint text' }]
    patches = installFetchMock({ boards: [codeBoard] })
    const { unmount } = await renderCockpit()
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('code')

    fireEvent.contextMenu(screen.getByTestId('gate-node-gate_review'), { clientX: 400, clientY: 200 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit gate' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit gate' })
    expect(within(dialog).getByLabelText('Evaluator profile')).toHaveValue('output_absent@1')
    expect(within(dialog).getByLabelText('Forbidden text')).toHaveValue('complaint text')

    fireEvent.click(within(dialog).getByLabelText('Human kind'))
    fireEvent.click(within(dialog).getByLabelText('Code kind'))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save gate' }))

    await waitFor(() => {
      expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toEqual({
        id: 'gate_review',
        title: 'Review',
        kinds: ['human'],
        criterion: 'Review the frame',
        check: '',
        checkVersion: '',
        checkValue: '',
      })
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit gate' })).toBeNull())
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('human')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toEqual({
        id: 'gate_review',
        title: 'Review',
        kinds: ['code'],
        criterion: 'Review the frame',
        check: 'output_absent',
        checkVersion: '1',
        checkValue: 'complaint text',
      })
    })

    fireEvent.contextMenu(screen.getByTestId('gate-node-gate_review'), { clientX: 400, clientY: 200 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit gate' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Edit gate' })).getByLabelText('Human kind'))
    fireEvent.click(screen.getByLabelText('Code kind'))
    fireEvent.click(screen.getByRole('button', { name: 'Save gate' }))
    await waitFor(() => expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('human'))
    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('gate-kinds-gate_review')).toHaveTextContent('human')
  })

  it('opens Edit gate on double-click and restores a detached judge chain on undo', async () => {
    const judgedBoard = makeBoard()
    judgedBoard.gates = [{ ...gate, kinds: ['code', 'formation'] }]
    patches = installFetchMock({ boards: [judgedBoard] })
    await renderCockpit()
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent('codejudge')
    expect(await screen.findByTestId('formation-wire-edge_judge_send')).toBeInTheDocument()

    fireEvent.doubleClick(screen.getByTestId('gate-node-gate_review'))
    const dialog = await screen.findByRole('dialog', { name: 'Edit gate' })
    expect(within(dialog).getByLabelText('Judge kind')).toBeChecked()
    fireEvent.click(within(dialog).getByLabelText('Judge kind'))
    expect(within(dialog).getByRole('note')).toHaveTextContent('Saving detaches the current judge chain.')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save gate' }))

    await waitFor(() => expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toMatchObject({ kinds: ['code'] }))
    await waitFor(() => expect(screen.queryByTestId('formation-wire-edge_judge_send')).toBeNull())
    expect(screen.getByTestId('gate-node-gate_review')).not.toHaveClass('hasjudge')
    expect(screen.getByTestId('gate-kinds-gate_review')).toHaveTextContent(/^code$/)

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.find(patch => patch.body.setGateJudge)?.body.setGateJudge).toEqual({ gateId: 'gate_review', chain: ['fmn_judge'] })
    })
    const restore = patches.findIndex(patch => (patch.body.updateGate as { kinds?: string[] } | undefined)?.kinds?.includes('formation'))
    expect(restore).toBeGreaterThan(-1)
    expect(restore).toBeLessThan(patches.findIndex(patch => patch.body.setGateJudge))
  })

  it('renames a formation inline, undoes it, and the title survives reload', async () => {
    const { unmount } = await renderCockpit()
    const card = screen.getByTestId('formation-node-fmn_frame')
    fireEvent.doubleClick(within(card).getByText('Frame'))
    const input = within(card).getByRole('textbox', { name: 'Rename Frame' })
    expect(input).toHaveValue('Frame')
    fireEvent.change(input, { target: { value: '  Map the territory ' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => {
      expect(patches.find(patch => patch.body.updateFormation)?.body.updateFormation).toEqual({ id: 'fmn_frame', title: 'Map the territory' })
    })
    expect(await within(card).findByText('Map the territory')).toBeInTheDocument()
    expect(patches.filter(patch => patch.url.endsWith('/layout'))).toEqual([])

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateFormation).map(patch => patch.body.updateFormation)).toEqual([
        { id: 'fmn_frame', title: 'Map the territory' },
        { id: 'fmn_frame', title: 'Frame' },
      ])
    })

    const renamed = within(screen.getByTestId('formation-node-fmn_frame'))
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_frame'), { clientX: 420, clientY: 120 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rename' }))
    const again = renamed.getByRole('textbox', { name: 'Rename Frame' })
    fireEvent.change(again, { target: { value: 'Question peers' } })
    fireEvent.blur(again)
    await waitFor(() => expect(renamed.getByText('Question peers')).toBeInTheDocument())

    unmount()
    await renderCockpit()
    expect(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Question peers')).toBeInTheDocument()
  })

  it('cancels an inline rename with Escape and skips unchanged titles', async () => {
    await renderCockpit()
    const gateCard = screen.getByTestId('gate-node-gate_review')
    fireEvent.doubleClick(within(gateCard).getByText('Review'))
    expect(screen.queryByRole('dialog', { name: 'Edit gate' })).toBeNull()
    const input = within(gateCard).getByRole('textbox', { name: 'Rename Review' })
    fireEvent.change(input, { target: { value: 'Discard me' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(within(gateCard).queryByRole('textbox')).toBeNull()
    expect(within(gateCard).getByText('Review')).toBeInTheDocument()

    fireEvent.doubleClick(within(gateCard).getByText('Review'))
    fireEvent.keyDown(within(gateCard).getByRole('textbox', { name: 'Rename Review' }), { key: 'Enter' })
    fireEvent.doubleClick(within(gateCard).getByText('Review'))
    fireEvent.change(within(gateCard).getByRole('textbox', { name: 'Rename Review' }), { target: { value: 'Framing review' } })
    fireEvent.keyDown(within(gateCard).getByRole('textbox', { name: 'Rename Review' }), { key: 'Enter' })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateGate).map(patch => patch.body.updateGate)).toEqual([{ id: 'gate_review', title: 'Framing review' }])
    })
  })

  it('edits a mission goal and Bead ID, undoes it, and the change survives reload', async () => {
    const { unmount } = await renderCockpit()
    fireEvent.doubleClick(screen.getByTestId('mission-node-mis_showcase'))
    const dialog = await screen.findByRole('dialog', { name: 'Edit mission' })
    expect(within(dialog).getByLabelText('Mission title')).toHaveValue('Showcase')
    expect(within(dialog).getByLabelText('Mission goal')).toHaveValue('Build the page')
    expect(within(dialog).getByLabelText('Mission Bead ID')).toHaveValue('home-7kc4.5')

    fireEvent.change(within(dialog).getByLabelText('Mission Bead ID'), { target: { value: 'Not A Bead' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save mission' }))
    expect(await within(dialog).findByText('Enter a Beads issue ID such as ctx-ug7.25, or leave it blank.')).toBeInTheDocument()
    expect(patches.filter(patch => patch.body.updateMission)).toEqual([])

    fireEvent.change(within(dialog).getByLabelText('Mission goal'), { target: { value: 'Draft a framing for review' } })
    fireEvent.change(within(dialog).getByLabelText('Mission Bead ID'), { target: { value: '' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save mission' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.updateMission)?.body.updateMission).toEqual({
        id: 'mis_showcase', title: 'Showcase', goal: 'Draft a framing for review', beadId: '',
      })
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit mission' })).toBeNull())
    expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Draft a framing for review')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateMission).slice(-1)[0]?.body.updateMission).toEqual({
        id: 'mis_showcase', title: 'Showcase', goal: 'Build the page', beadId: 'home-7kc4.5',
      })
    })

    fireEvent.contextMenu(screen.getByTestId('mission-node-mis_showcase'), { clientX: 120, clientY: 120 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit mission' }))
    const second = await screen.findByRole('dialog', { name: 'Edit mission' })
    fireEvent.change(within(second).getByLabelText('Mission goal'), { target: { value: 'Map the territory first' } })
    fireEvent.click(within(second).getByRole('button', { name: 'Save mission' }))
    await waitFor(() => expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Map the territory first'))

    unmount()
    await renderCockpit()
    expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Map the territory first')
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
