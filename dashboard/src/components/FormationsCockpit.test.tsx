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
}

const formation = {
  id: 'fmn_frame',
  type: 'orchestrated',
  title: 'Frame',
  inputs: [{ id: 'port_frame_in', label: 'Input' }],
  outputs: [{ id: 'port_frame_out', label: 'Output' }],
  slots: [
    { id: 'slot_lead', label: 'Lead', controller: true, agentId: 'mason', harness: 'codex', model: 'mason-model', effort: 'medium' },
    { id: 'slot_worker', label: 'Worker', controller: false },
  ],
}

type TestGate = { id: string; title: string; kinds: string[]; criterion: string; check?: string; checkVersion?: string; checkValue?: string; files?: string[] }
const gate: TestGate = { id: 'gate_review', title: 'Review', kinds: ['code'], criterion: 'Review the frame' }
const mission = { id: 'mis_showcase', title: 'Showcase', goal: 'Build the page' }
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
    inputCards: [mission],
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
  missionId: 'brd_test',
  missionRev: 7,
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
let recordedFetches: string[] = []

function installFetchMock(options: {
  emptyBoards?: boolean
  freshCreateLayout?: boolean
  missionCreateFailure?: boolean
  wireFailure?: boolean
  boards?: TestBoard[]
  sameBoardRefreshes?: TestBoard[]
  runEvents?: TestRunEvent[]
  escalations?: TestEscalation[]
  runStatus?: TestRunStatus
  boardNotes?: { mission?: TestNoteEntry[]; elements?: Array<{ nodeId: string; entries: TestNoteEntry[] }> }
  notePatchConflict?: boolean
  agents?: typeof agents
  agentDetailGate?: Promise<void>
  validation?: { errors: TestFinding[]; warnings: TestFinding[] }
  runStartFindings?: TestFinding[]
  restoreFailure?: boolean
  /** The first PATCH carrying this op answers a stale-revision CONFLICT, as after another editor's write. */
  conflictOnce?: string
  /** addPort answers only once this settles, so an edit is in flight. */
  addPortGate?: Promise<void>
  /** updateLimit answers 400 INVALID_LIMIT with this message, as the store refuses a write. */
  limitRefusal?: string
} = {}) {
  let conflictPending = options.conflictOnce
  const patches: RecordedPatch[] = []
  recordedMutations = []
  recordedFetches = []
  let availableBoards = options.emptyBoards ? [] : (options.boards?.length ? options.boards : [makeBoard()])
  let board = availableBoards[0] || makeBoard()
  let availableAgents = options.agents || agents
  let currentLayout = layout
  let boardNotes = {
    schema: 2,
    missionId: board.id,
    rev: options.boardNotes ? 1 : 0,
    updatedAt: '2026-08-18T13:00:00Z',
    updatedBy: 'human:test',
    mission: options.boardNotes?.mission || [] as TestNoteEntry[],
    elements: options.boardNotes?.elements || [] as Array<{ nodeId: string; entries: TestNoteEntry[] }>,
    etag: options.boardNotes ? 'notes-etag' : '*',
  }
  let nextNoteID = 1
  ;(globalThis as Record<string, unknown>).fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = (init?.method || 'GET').toUpperCase()
    if (method !== 'GET') recordedMutations.push({ method, url })
    else recordedFetches.push(url)
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
    if (method === 'POST' && url === '/api/missions') {
      const body = JSON.parse(String(init?.body)) as { title: string }
      const created = {
        ...makeBoard(),
        id: 'brd_created',
        slug: 'release-plan',
        title: body.title,
        rev: 1,
        etag: 'created-board-etag',
        inputCards: [],
        formations: [],
        gates: [],
        connections: [],
      }
      availableBoards = [...availableBoards, created]
      board = created
      currentLayout = { schema: 1, missionId: created.id, missionRev: created.rev, etag: '*', nodes: [], edges: [] }
      boardNotes = { ...boardNotes, missionId: created.id, rev: 0, mission: [], elements: [], etag: '*' }
      return respond({ mission: created }, created.etag)
    }
    if (method === 'DELETE' && url.includes('/api/missions/')) {
      availableBoards = availableBoards.filter(item => item.slug !== board.slug)
      return respond({ deletion: { id: board.id, slug: board.slug, title: board.title, archiveId: 'archive_test' } })
    }
    if (method === 'GET' && /^\/api\/missions\/[^/]+\/notes$/.test(url)) {
      return respond({ notes: boardNotes }, boardNotes.etag)
    }
    if (method === 'PATCH' && /^\/api\/missions\/[^/]+\/notes$/.test(url)) {
      if (options.notePatchConflict) return conflict('Shared notes changed; reload and retry')
      const body = JSON.parse(String(init?.body)) as { target: string; action: string; entryId?: string; text?: string; author: string }
      const thread = body.target === 'mission' ? boardNotes.mission : boardNotes.elements.find(note => note.nodeId === body.target)?.entries || []
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
        mission: body.target === 'mission' ? nextThread : boardNotes.mission,
        elements: body.target === 'mission'
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
      // As the daemon reads a card: no launch string, plus the derived seat launch.
      const defaultVariant = {
        id: harness,
        model: `${agent.id}-model`,
        effort: 'medium',
        sessionStem: agent.id,
        effectiveEffort: 'medium',
        efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
        seatLaunch: `exec '/usr/bin/${harness === 'openai-codex' ? 'codex' : 'claude'}' --model '${agent.id}-model'`,
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
      if (body.wireConnection || body.rewireConnection) {
        if (options.wireFailure) return reject('Input already has a feed')
        const edit = (body.wireConnection || body.rewireConnection) as { from: string; to: string; previousTo?: string; joinIfOccupied?: boolean; removePreviousInput?: boolean }
        const connections = board.connections.filter(edge => !(edit.previousTo && edge.from === edit.from && edge.to === edit.previousTo))
        let to = edit.to
        let formations = structuredClone(board.formations)
        if (connections.some(edge => edge.to === to)) {
          const target = formations.find(item => item.id === to.split(':')[0])
          if (!target || !edit.joinIfOccupied) return reject('Input already has a feed')
          const port = { id: `join_${board.rev}`, label: 'Input' }
          target.inputs.push(port)
          to = `${target.id}:${port.id}`
        }
        if (edit.removePreviousInput) {
          const [nodeId, portId] = edit.previousTo!.split(':')
          formations = formations.map(item => item.id === nodeId ? { ...item, inputs: item.inputs.filter(port => port.id !== portId) } : item)
        }
        board = { ...board, formations, rev: board.rev + 1, connections: [...connections, { id: `edge_${board.rev}`, from: edit.from, to }] }
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (conflictPending && body[conflictPending]) {
        conflictPending = undefined
        return conflict('Formation definition changed; reload and retry')
      }
      if (body.addPort && options.addPortGate) {
        const gateOpen = options.addPortGate
        options = { ...options, addPortGate: undefined }
        return gateOpen.then(() => (globalThis.fetch as unknown as (input: string, init: RequestInit) => Promise<unknown>)(url, init!))
      }
      // Deletes and restores mirror the store: a delete drops the node, its
      // connections and its layout node; a restore refuses a node already there.
      const nodeKinds = [['deleteFormation', 'formations'], ['deleteGate', 'gates'], ['deleteInputCard', 'inputCards']] as const
      for (const [op, key] of nodeKinds) {
        if (!body[op]) continue
        const { id } = body[op] as { id: string }
        const nodes = board[key] as Array<{ id: string }>
        if (!nodes.some(node => node.id === id)) return Promise.resolve({
          ok: false, status: 404, headers: { get: () => null },
          json: () => Promise.resolve({ success: false, error: { code: 'NOT_FOUND', message: 'Formation resource not found' } }),
          text: () => Promise.resolve(''),
        })
        board = { ...board, rev: board.rev + 1, [key]: nodes.filter(node => node.id !== id),
          connections: board.connections.filter(edge => edge.from.split(':')[0] !== id && edge.to.split(':')[0] !== id) }
        currentLayout = { ...currentLayout, missionRev: board.rev, nodes: currentLayout.nodes.filter(node => node.id !== id) }
        return respond({ mission: board, layout: currentLayout }, `board-${board.rev}`)
      }
      if (body.restoreNode) {
        const restore = body.restoreNode as { inputCard?: { id: string }; formation?: { id: string }; gate?: { id: string }; connections: TestBoard['connections']; x: number; y: number }
        const key = restore.inputCard ? 'inputCards' : restore.formation ? 'formations' : 'gates'
        const node = (restore.inputCard || restore.formation || restore.gate)!
        const taken = [...board.inputCards, ...board.formations, ...board.gates].some(item => item.id === node.id)
        if (taken || options.restoreFailure) return Promise.resolve({
          ok: false, status: 409, headers: { get: () => null },
          json: () => Promise.resolve({ success: false, error: { code: 'INVALID_NODE_RESTORE', message: `node "${node.id}" is already in the mission` } }),
          text: () => Promise.resolve(''),
        })
        board = { ...board, rev: board.rev + 1, [key]: [...(board[key] as unknown[]), node], connections: [...board.connections, ...restore.connections] } as TestBoard
        currentLayout = { ...currentLayout, missionRev: board.rev, nodes: [...currentLayout.nodes, { id: node.id, x: restore.x, y: restore.y }] }
        return respond({ mission: board, layout: currentLayout, nodeId: node.id }, `board-${board.rev}`)
      }
      if (body.restorePort) {
        const { formationId, direction, port, index, connections } = body.restorePort as { formationId: string; direction: string; port: { id: string; label: string }; index: number; connections: TestBoard['connections'] }
        const key = direction === 'input' ? 'inputs' : 'outputs'
        board = { ...board, rev: board.rev + 1,
          formations: board.formations.map(item => item.id !== formationId ? item : { ...item, [key]: [...item[key].slice(0, index), port, ...item[key].slice(index)] }),
          connections: [...board.connections, ...connections],
        }
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (body.addPort) {
        // Mirrors the store: only input and output are directions (FormationPortInput/Output).
        const { formationId, direction, label } = body.addPort as { formationId: string; direction: string; label?: string }
        if (direction !== 'input' && direction !== 'output') {
          return Promise.resolve({
            ok: false,
            status: 400,
            headers: { get: () => null },
            json: () => Promise.resolve({ success: false, error: { code: 'INVALID_PORT_DIRECTION', message: `port direction "${direction}" must be input or output` } }),
            text: () => Promise.resolve(''),
          })
        }
        const key = direction === 'input' ? 'inputs' : 'outputs'
        const port = { id: `port_added_${board.rev}`, label: label || (direction === 'input' ? 'Input' : 'Output') }
        board = { ...board, rev: board.rev + 1,
          formations: board.formations.map(item => item.id === formationId ? { ...item, [key]: [...item[key], port] } : item),
        }
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (body.removePort) {
        const { formationId, portId } = body.removePort as { formationId: string; portId: string }
        board = { ...board, rev: board.rev + 1,
          formations: board.formations.map(item => item.id === formationId ? { ...item, inputs: item.inputs.filter(port => port.id !== portId), outputs: item.outputs.filter(port => port.id !== portId) } : item),
          connections: board.connections.filter(edge => edge.to !== `${formationId}:${portId}` && edge.from !== `${formationId}:${portId}`),
        }
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (body.unwireConnection) {
        const { from, to } = body.unwireConnection as { from: string; to: string }
        board = { ...board, rev: board.rev + 1, connections: board.connections.filter(edge => edge.from !== from || edge.to !== to) }
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (!url.endsWith('/layout') && typeof body.title === 'string') {
        board = { ...board, title: body.title, rev: board.rev + 1, etag: 'board-etag-2' }
        availableBoards = availableBoards.map(item => item.slug === board.slug ? board : item)
        return respond({ mission: board }, board.etag)
      }
      if (!url.endsWith('/layout') && body.createInputCard) {
        if (options.missionCreateFailure) return reject('Mission create failed')
        const requested = body.createInputCard as { title: string; goal: string; x: number; y: number }
        const created = {
          id: 'mis_created',
          title: requested.title,
          goal: requested.goal,
        }
        board = { ...board, rev: board.rev + 1, inputCards: [...board.inputCards, created] }
        currentLayout = {
          ...currentLayout,
          missionRev: board.rev,
          etag: 'layout-mission-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: requested.x, y: requested.y }],
        }
        return respond({ mission: board, layout: currentLayout }, 'board-etag-2')
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
        return respond({ mission: board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.setBrief) {
        const { formationId, ...brief } = body.setBrief as { formationId: string; goal: string; beadId: string; files: string[]; links: string[] }
        board = { ...board, rev: board.rev + 1, formations: board.formations.map(item => item.id === formationId ? { ...item, brief } : item) as TestBoard['formations'] }
        return respond({ mission: board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.assignSlot) {
        const { formationId, slotId, agentId, harness, model, effort } = body.assignSlot as { formationId: string; slotId: string; agentId: string; harness: string; model?: string; effort?: string }
        // Mirrors the store's role drag: a patch naming only a role (and perhaps
        // a harness) takes that role card's settings, as served above.
        const roleDrag = Boolean(agentId) && model === undefined && effort === undefined
        const settings = roleDrag ? { model: `${agentId}-model`, effort: 'medium' } : { model: model || undefined, effort: effort || undefined }
        board = {
          ...board,
          rev: board.rev + 1,
          formations: board.formations.map(item => item.id !== formationId ? item : {
            ...item,
            slots: item.slots.map(slot => slot.id === slotId ? { ...slot, agentId: agentId || undefined, harness: harness || undefined, ...settings } : slot),
          }) as TestBoard['formations'],
        }
        return respond({ mission: board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.updateLimit) {
        // Mirrors the store (UpdateLimit): a knob of 0 clears it, '' unwires.
        const { id, ...change } = body.updateLimit as { id: string; title?: string; target?: string; rounds?: number; seconds?: number; warnSeconds?: number }
        if (options.limitRefusal) return Promise.resolve({
          ok: false, status: 400, headers: { get: () => null },
          json: () => Promise.resolve({ success: false, error: { code: 'INVALID_LIMIT', message: options.limitRefusal } }),
          text: () => Promise.resolve(''),
        })
        const limits = ((board as { limits?: Array<{ id: string; title: string; target: string; rounds?: number; seconds?: number; warnSeconds?: number }> }).limits || []).map(item => {
          if (item.id !== id) return item
          const next = { ...item, ...change }
          for (const knob of ['rounds', 'seconds', 'warnSeconds'] as const) if (change[knob] === 0) delete next[knob]
          return next
        })
        board = { ...board, rev: board.rev + 1, limits } as TestBoard
        return respond({ mission: board }, `board-${board.rev}`)
      }
      if (!url.endsWith('/layout') && body.updateFormation) {
        const { id, title } = body.updateFormation as { id: string; title: string }
        board = { ...board, rev: board.rev + 1, formations: board.formations.map(item => item.id === id ? { ...item, title } : item) }
        return respond({ mission: board }, 'board-etag-2')
      }
      if (!url.endsWith('/layout') && body.updateInputCard) {
        const { id, ...fields } = body.updateInputCard as { id: string; title?: string; goal?: string; beadId?: string }
        board = { ...board, rev: board.rev + 1, inputCards: board.inputCards.map(item => item.id === id ? { ...item, ...fields } : item) }
        return respond({ mission: board }, 'board-etag-2')
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
        return respond({ mission: board }, 'board-etag-2')
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
        return respond({ mission: board }, 'board-etag-2')
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
        }
        board = { ...board, rev: board.rev + 1, formations: [...board.formations, created] as TestBoard['formations'] }
        currentLayout = {
          ...currentLayout,
          missionRev: board.rev,
          etag: 'layout-formation-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: requested.x, y: requested.y }],
        }
        return respond({ mission: board, layout: currentLayout }, 'board-etag-2')
      }
      if (options.freshCreateLayout && !url.endsWith('/layout') && body.createGate) {
        const requested = body.createGate as { title: string; kinds: string[]; criterion: string; check: string; checkVersion: string; checkValue: string }
        const created = { id: 'gate_created', title: requested.title, kinds: requested.kinds, criterion: requested.criterion, check: requested.check, checkVersion: requested.checkVersion, checkValue: requested.checkValue }
        board = { ...board, rev: board.rev + 1, gates: [...board.gates, created] }
        currentLayout = {
          ...currentLayout,
          missionRev: board.rev,
          etag: 'layout-created-etag',
          nodes: [...currentLayout.nodes, { id: created.id, x: 1344, y: 784 }],
        }
        return respond({ mission: board, layout: currentLayout }, 'board-etag-2')
      }
      board = { ...board, rev: board.rev + 1 }
      if (url.endsWith('/layout')) return respond({ layout: currentLayout }, 'layout-etag-2')
      return respond({ mission: board }, 'board-etag-2')
    }
    if (url === '/api/runs' && init?.method === 'POST' && options.runStartFindings) {
      const findings = options.runStartFindings
      return Promise.resolve({
        ok: false,
        status: 422,
        headers: { get: () => null },
        json: () => Promise.resolve({ success: false, error: { code: 'RUN_ADMISSION_FAILED', message: `The run needs ${findings.length} fixes before it can start`, findings } }),
        text: () => Promise.resolve(''),
      })
    }
    if (url === '/api/runs' && init?.method === 'POST') return respond({ runId: 'run_legacy' })
    if (/\/api\/runs\/[^/]+\/escalations$/.test(url)) return respond({ escalations: options.escalations || [] })
    if (/\/api\/runs\/[^/]+\/events$/.test(url)) return respond({ events: options.runEvents || [] })
    if (/\/api\/runs\/[^/]+$/.test(url)) {
      return respond({ status: {
        runId: 'run_legacy',
        status: options.runStatus?.status ?? 'succeeded',
        final: options.runStatus?.final ?? true,
        resumeAllowed: options.runStatus?.resumeAllowed ?? false,
        missionSlug: board.slug,
        inputCardId: mission.id,
        eventCount: options.runEvents?.length || 0,
      } })
    }
    if (url === '/api/gate-profiles') {
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
    if (url === '/api/missions') return respond({ missions: availableBoards.map(item => ({ id: item.id, slug: item.slug, title: item.title, rev: item.rev, etag: item.etag })) })
    if (url.includes('/changes')) {
      const refreshedBoard = options.sameBoardRefreshes?.shift()
      if (!refreshedBoard) return respond({ signal: { changed: false } })
      board = refreshedBoard
      currentLayout = { ...currentLayout, missionRev: refreshedBoard.rev }
      return respond({
        signal: {
          mission: refreshedBoard.slug,
          changed: true,
          rev: refreshedBoard.rev,
          etag: refreshedBoard.etag,
        },
      })
    }
    if (url.endsWith('/layout')) return respond({ layout: currentLayout }, 'layout-etag')
    if (url.endsWith('/validation')) {
      return respond({ missionRev: board.rev, missionEtag: board.etag, errors: options.validation?.errors || [], warnings: options.validation?.warnings || [] })
    }
    if (url.includes('/api/missions/')) {
      const requested = url.includes(`/missions/${board.slug}`)
        ? board
        : availableBoards.find(item => url.includes(`/missions/${item.slug}`)) || board
      return respond({ mission: requested }, requested.etag)
    }
    if (url === '/api/agents') return respond({ agents: availableAgents })
    return respond({})
  }) as unknown as typeof fetch
  return patches
}

/** A mission saved before its Input card was added: the canvas offers the Input card. */
function missionlessBoard() {
  const board = makeBoard()
  return { ...board, inputCards: [], connections: board.connections.filter(edge => !edge.from.startsWith(`${mission.id}:`)) }
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

  it.each([false, true])('joins a fed input and undoes the port and wire together (reconnect=%s)', async reconnect => {
    const { container } = await renderCockpit()
    const source = container.querySelector<HTMLElement>(reconnect ? '[data-port-in="fmn_frame:port_frame_in"]' : '[data-port-out="fmn_frame:port_frame_out"]')!
    const target = container.querySelector<HTMLElement>('[data-port-in="fmn_judge:port_judge_in"]')!
    Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: () => target })
    try {
      fireEvent.pointerDown(source, { button: 0, pointerId: 77, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(window, { pointerId: 77, clientX: 600, clientY: 400 })
      fireEvent.pointerUp(window, { pointerId: 77, clientX: 600, clientY: 400 })
      await waitFor(() => expect(container.querySelectorAll('[data-port-in^="fmn_judge:"]')).toHaveLength(2))
      expect(patches.map(patch => patch.body[reconnect ? 'rewireConnection' : 'wireConnection']).filter(Boolean)).toEqual([
        expect.objectContaining({ to: 'fmn_judge:port_judge_in', joinIfOccupied: true }),
      ])
      fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
      await waitFor(() => expect(container.querySelectorAll('[data-port-in^="fmn_judge:"]')).toHaveLength(1))
      if (reconnect) {
        expect(patches[patches.length - 1]?.body.rewireConnection).toEqual({ from: 'mis_showcase:out', previousTo: 'fmn_judge:join_7', to: 'fmn_frame:port_frame_in', removePreviousInput: true })
        expect(screen.getByTestId('formation-node-fmn_frame').querySelector('[data-port-in].has')).not.toBeNull()
      } else {
        expect(patches[patches.length - 1]?.body.removePort).toEqual({ formationId: 'fmn_judge', portId: 'join_7' })
      }
      const count = patches.length
      fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
      await act(async () => { await Promise.resolve() })
      expect(patches).toHaveLength(count)
    } finally {
      delete (document as { elementFromPoint?: unknown }).elementFromPoint
    }
  })

  it.each([false, true])('records no undo after a failed wire (reconnect=%s)', async reconnect => {
    patches = installFetchMock({ wireFailure: true })
    const { container } = await renderCockpit()
    const source = container.querySelector<HTMLElement>(reconnect ? '[data-port-in="fmn_frame:port_frame_in"]' : '[data-port-out="fmn_frame:port_frame_out"]')!
    const target = container.querySelector<HTMLElement>('[data-port-in="fmn_judge:port_judge_in"]')!
    Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: () => target })
    try {
      fireEvent.pointerDown(source, { button: 0, pointerId: 78, clientX: 300, clientY: 200 })
      fireEvent.pointerMove(window, { pointerId: 78, clientX: 600, clientY: 400 })
      fireEvent.pointerUp(window, { pointerId: 78, clientX: 600, clientY: 400 })
      await screen.findByText('Input already has a feed')
      const count = patches.length
      fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
      await act(async () => { await Promise.resolve() })
      expect(patches).toHaveLength(count)
      expect(container.querySelectorAll('[data-port-in^="fmn_judge:"]')).toHaveLength(1)
    } finally {
      delete (document as { elementFromPoint?: unknown }).elementFromPoint
    }
  })

  it('labels gate wires beside the gate, draws a loop as a dashed back-reference and names the judge chain', async () => {
    const base = makeBoard()
    const ship = { ...judgeFormation, id: 'fmn_ship', title: 'Ship', inputs: [{ id: 'port_ship_in', label: 'Input' }], outputs: [{ id: 'port_ship_out', label: 'Output' }], slots: [] }
    patches = installFetchMock({ boards: [{
      ...base,
      formations: [...base.formations, ship],
      connections: [
        ...base.connections,
        { id: 'edge_review_pass', from: 'gate_review:pass', to: 'fmn_ship:port_ship_in' },
        { id: 'edge_review_fail', from: 'gate_review:fail', to: 'fmn_frame:port_frame_in' },
      ],
    }] })
    await renderCockpit()

    await waitFor(() => expect(screen.getByTestId('wire-label-edge_review_fail')).toHaveTextContent('↺ Frame'))
    expect(screen.getByTestId('formation-wire-edge_review_fail')).toHaveClass('wire', 'fail', 'loop')
    expect(screen.getByTestId('wire-label-edge_review_fail')).toHaveClass('loop')
    // jsdom lays nothing out, so Ship sits beside the gate and its pass wire needs no label.
    expect(screen.queryByTestId('wire-label-edge_review_pass')).toBeNull()
    expect(screen.getByTestId('formation-wire-edge_review_pass')).not.toHaveClass('loop')
    expect(screen.getByTestId('wire-label-edge_judge_send')).toHaveTextContent('judges Review')
    expect(screen.queryByTestId('wire-label-edge_judge_return')).toBeNull()
    expect(screen.queryByTestId('wire-label-edge_frame_gate')).toBeNull()
  })

  it('explains the canvas notation in a legend and staffing in slot tooltips', async () => {
    await renderCockpit()
    expect(screen.getByTestId('slot-fmn_frame-slot_lead')).toHaveAttribute('title', 'mason · Codex')
    expect(screen.getByTestId('slot-fmn_frame-slot_worker')).toHaveAttribute('title', 'Worker: open slot. Drag a persona here to staff it.')

    const toggle = screen.getByRole('button', { name: 'Legend' })
    fireEvent.click(toggle)
    const legend = screen.getByRole('dialog', { name: 'Canvas legend' })
    for (const words of ['A gate sends work back to an earlier step', 'A judge chain: the gate asks a formation to decide', 'You decide', 'Waiting for your answer', 'Blocked or failed; the run bar says why', 'A pause after your answer', 'Claude Code', 'Codex', 'Hermes']) {
      expect(legend).toHaveTextContent(words)
    }
    expect(legend.querySelector('path.wire.fail.loop')).not.toBeNull()
    expect(within(legend).getByText('↺ Earlier step')).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Canvas legend' })).toBeNull()
    expect(patches).toEqual([])
  })

  // The reusable unit is a mission and its entry node is the Input card; no
  // label, button, menu or accessible name calls either of them a board.
  it('names the unit a mission and its entry the Input card, never a board', async () => {
    patches = installFetchMock({ boards: [{ ...makeBoard(), title: 'Delivery' }] })
    const { container } = await renderCockpit()
    const accessibleText = () => [container.textContent || '', ...[...container.querySelectorAll('[aria-label],[title],[placeholder]')]
      .flatMap(element => ['aria-label', 'title', 'placeholder'].map(name => element.getAttribute(name) || ''))].join('\n')
    expect(screen.getByRole('combobox', { name: 'Mission' })).toHaveValue('test-board')
    expect(screen.getByTestId('new-board')).toHaveTextContent('New mission')
    expect(screen.getByRole('button', { name: 'Mission notes' })).toBeInTheDocument()
    expect(screen.getByTestId(`mission-node-${mission.id}`)).toHaveTextContent('◆ Input')
    fireEvent.contextMenu(screen.getByTestId(`mission-node-${mission.id}`))
    const actions = await screen.findByRole('menu', { name: 'Input card actions' })
    expect(within(actions).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Start mission', 'Delete Input card'])
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(accessibleText()).not.toMatch(/\bboards?\b/i)
    fireEvent.click(screen.getByRole('radio', { name: 'Flow' }))
    await screen.findByRole('region', { name: 'Input card Showcase' })
    expect(accessibleText()).not.toMatch(/\bboards?\b/i)
  })

  it('does not fabricate a starter board when no real boards exist', async () => {
    patches = installFetchMock({ emptyBoards: true })
    render(<FormationsCockpit />)
    expect(await screen.findByTestId('formations-empty-board')).toHaveTextContent('No missions yet')
    expect(screen.getByTestId('board-picker')).toHaveTextContent('No missions')
    expect(screen.queryByText('Improve session search')).toBeNull()
    expect(screen.getByTestId('new-board')).toBeEnabled()
    expect(screen.getByTestId('new-formation')).toBeDisabled()
    expect(patches).toEqual([])
  })

  it('creates and selects a named blank mission from the top bar', async () => {
    patches = installFetchMock({ emptyBoards: true })
    render(<FormationsCockpit />)
    await screen.findByTestId('formations-empty-board')

    fireEvent.click(screen.getByTestId('new-board'))
    const dialog = await screen.findByRole('dialog', { name: 'Create mission' })
    fireEvent.change(within(dialog).getByLabelText('Mission name'), { target: { value: 'Release Plan' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create mission' }))

    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveValue('release-plan'))
    expect(screen.getByTestId('board-picker')).toHaveTextContent('Release Plan')
    expect(screen.getByTestId('formations-empty-board')).toHaveTextContent('This mission is empty')
    expect(recordedMutations).toContainEqual({ method: 'POST', url: '/api/missions' })
  })

  it('renames the selected board through the top-bar board controls', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Rename mission' }))
    const dialog = await screen.findByRole('dialog', { name: 'Rename mission' })
    expect(screen.getByTestId('board-picker')).toBeDisabled()
    const input = within(dialog).getByLabelText('Mission name')
    expect(input).toHaveValue('Test board')
    fireEvent.change(input, { target: { value: 'Delivery map' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save mission name' }))

    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveTextContent('Delivery map'))
    expect(patches.some(patch => patch.body.title === 'Delivery map')).toBe(true)
  })

  it('archives a board only after explicit confirmation', async () => {
    await renderCockpit()
    const trigger = screen.getByRole('button', { name: 'Delete mission' })
    fireEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: 'Delete mission' })
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(dialog).toHaveTextContent('archived')
    expect(recordedMutations.some(mutation => mutation.method === 'DELETE')).toBe(false)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive mission' }))
    await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveTextContent('No missions'))
    expect(recordedMutations).toContainEqual({ method: 'DELETE', url: '/api/missions/test-board' })
  })

  it('restores board-dialog trigger focus after Escape', async () => {
    await renderCockpit()
    const trigger = screen.getByTestId('new-board')
    fireEvent.click(trigger)
    expect(await screen.findByLabelText('Mission name')).toHaveFocus()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create mission' })).toBeNull())
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  it('rechecks note drafts before creating a board from an open dialog', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Mission notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the mission' })
    fireEvent.click(screen.getByTestId('new-board'))
    const dialog = await screen.findByRole('dialog', { name: 'Create mission' })
    fireEvent.change(within(dialog).getByLabelText('Mission name'), { target: { value: 'Should not create' } })
    fireEvent.change(boardNote, { target: { value: 'Draft made after dialog opened' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create mission' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create mission' })).toBeNull())
    expect(boardNote).toHaveValue('Draft made after dialog opened')
    expect(recordedMutations.some(mutation => mutation.method === 'POST' && mutation.url === '/api/missions')).toBe(false)
  })

  it('rechecks note drafts before archiving from an open dialog', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Mission notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the mission' })
    fireEvent.click(screen.getByRole('button', { name: 'Delete mission' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete mission' })
    fireEvent.change(boardNote, { target: { value: 'Draft made after delete opened' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive mission' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Delete mission' })).toBeNull())
    expect(boardNote).toHaveValue('Draft made after delete opened')
    expect(recordedMutations.some(mutation => mutation.method === 'DELETE')).toBe(false)
  })

  it('shows mixed-author note threads in note windows and replies without overwriting', async () => {
    patches = installFetchMock({
      boardNotes: {
        mission: [noteEntry('nte_board', 'human:ui', 'Preserve the API contract.')],
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

    fireEvent.click(screen.getByRole('button', { name: 'Mission notes' }))
    const boardWindow = await screen.findByRole('dialog', { name: 'mission notes' })
    expect(noteWindow).toBeInTheDocument()
    const boardThread = within(boardWindow).getByRole('list', { name: 'Mission note thread' })
    fireEvent.click(within(boardThread).getByRole('button', { name: /^Delete your note/ }))
    await waitFor(() => expect(within(boardWindow).queryByRole('list', { name: 'Mission note thread' })).toBeNull())
    expect(boardWindow).toHaveTextContent('No notes yet')

    fireEvent.click(within(noteWindow).getByRole('button', { name: 'Close notes for Frame' }))
    expect(screen.queryByRole('dialog', { name: 'notes for Frame' })).toBeNull()
    expect(screen.getByRole('dialog', { name: 'mission notes' })).toBeInTheDocument()
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
    expect(localStorage.getItem('archon.notesMode')).toBe('full')

    fireEvent.click(within(switcher).getByRole('radio', { name: 'Hide notes' }))
    expect(screen.queryByRole('note', { name: 'Notes for Frame' })).toBeNull()
    expect(within(screen.getByTestId('formation-node-fmn_frame')).getByRole('button', { name: 'Open notes for Frame' })).toBeInTheDocument()

    unmount()
    await renderCockpit()
    expect(screen.getByRole('radio', { name: 'Hide notes' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByRole('note', { name: 'Notes for Frame' })).toBeNull()
  })

  it('preserves a local note draft and offers an explicit reload after a repeated conflict', async () => {
    patches = installFetchMock({ boardNotes: { mission: [noteEntry('nte_server', 'agent:archon', 'Server version')] }, notePatchConflict: true })
    await renderCockpit()
    fireEvent.click(screen.getByRole('button', { name: 'Mission notes' }))
    const boardWindow = await screen.findByRole('dialog', { name: 'mission notes' })
    const boardNote = within(boardWindow).getByRole('textbox', { name: 'Note for the mission' })
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

    fireEvent.click(screen.getByRole('button', { name: 'Mission notes' }))
    const boardNote = await screen.findByRole('textbox', { name: 'Note for the mission' })
    fireEvent.change(boardNote, { target: { value: 'Unsaved local context' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close mission notes' }))
    expect(screen.queryByRole('dialog', { name: 'mission notes' })).toBeNull()
    fireEvent.change(screen.getByTestId('board-picker'), { target: { value: 'second-board' } })

    expect(screen.getByTestId('board-picker')).toHaveValue('test-board')
    const reopened = await screen.findByRole('dialog', { name: 'mission notes' })
    expect(within(reopened).getByRole('alert')).toHaveTextContent('Save the current notes before leaving this mission')
    expect(within(reopened).getByRole('textbox', { name: 'Note for the mission' })).toHaveValue('Unsaved local context')
    fireEvent.click(screen.getByRole('button', { name: 'Delete mission' }))
    expect(screen.queryByRole('dialog', { name: 'Delete mission' })).toBeNull()
    fireEvent.click(screen.getByTestId('new-board'))
    expect(screen.queryByRole('dialog', { name: 'Create mission' })).toBeNull()
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
    expect(screen.getByTestId('roster-count')).toHaveTextContent(/^2 · 1 on canvas$/)
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

  it('creates an Input card with no optional input, and it has no Bead ID field', async () => {
    patches = installFetchMock({ boards: [missionlessBoard()] })
    vi.spyOn(window, 'requestAnimationFrame').mockReturnValue(1)
    const { container, unmount } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    const menu = await screen.findByRole('menu', { name: 'New' })
    expect(within(menu).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Input card', 'Solo formation', 'Peer formation', 'Orchestrated formation', 'Gate', 'End node · done', 'End node · rejected', 'Limit card'])
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Input card' }))

    const dialog = await screen.findByRole('dialog', { name: 'Add Input card' })
    expect(menu).not.toBeInTheDocument()
    expect(patches.filter(patch => patch.body.createInputCard)).toEqual([])

    expect(within(dialog).queryByLabelText('Mission Bead ID')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add Input card' }))

    await waitFor(() => {
      const create = patches.find(patch => patch.body.createInputCard)
      expect(create?.body.createInputCard).toEqual({
        title: 'Test board',
        goal: '',
        x: 1260,
        y: 252,
      })
    })
    expect(dialog).not.toBeInTheDocument()
    expect(await screen.findByTestId('mission-node-mis_created')).toHaveTextContent('◆ InputTest board')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.some(patch => (patch.body.deleteInputCard as { id?: string } | undefined)?.id === 'mis_created')).toBe(true)
    })
    await waitFor(() => expect(screen.queryByTestId('mission-node-mis_created')).toBeNull())

    // The undone create stays undone after a reload.
    unmount()
    await renderCockpit()
    expect(screen.queryByTestId('mission-node-mis_created')).toBeNull()
  })

  it('creates an Input card with its title and goal trimmed', async () => {
    patches = installFetchMock({ boards: [missionlessBoard()] })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Input card' }))
    await screen.findByRole('dialog', { name: 'Add Input card' })
    fireEvent.change(screen.getByLabelText('Input card title'), { target: { value: '  Plan release  ' } })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: '  Ship reduced candidate  ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add Input card' }))
    await waitFor(() => {
      expect(patches.find(patch => patch.body.createInputCard)?.body.createInputCard).toMatchObject({
        title: 'Plan release',
        goal: 'Ship reduced candidate',
      })
    })
  })

  it('cancels Mission creation with Cancel and Escape without mutation', async () => {
    patches = installFetchMock({ boards: [missionlessBoard()] })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement

    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Input card' }))
    await screen.findByRole('dialog', { name: 'Add Input card' })
    fireEvent.change(screen.getByLabelText('Input card title'), { target: { value: 'Discard me' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel adding the Input card' }))
    expect(screen.queryByRole('dialog', { name: 'Add Input card' })).toBeNull()

    fireEvent.contextMenu(viewport, { clientX: 360, clientY: 360 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Input card' }))
    await screen.findByRole('dialog', { name: 'Add Input card' })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: 'Discard this too' } })
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Add Input card' })).toBeNull())

    expect(patches.filter(patch => patch.body.createInputCard)).toEqual([])
  })

  it('retains the Mission draft after the API rejects creation', async () => {
    patches = installFetchMock({ missionCreateFailure: true, boards: [missionlessBoard()] })
    const { container } = await renderCockpit()
    const viewport = container.querySelector('.viewport') as HTMLElement
    fireEvent.contextMenu(viewport, { clientX: 300, clientY: 300 })
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Input card' }))
    await screen.findByRole('dialog', { name: 'Add Input card' })

    fireEvent.change(screen.getByLabelText('Input card title'), { target: { value: 'Plan release' } })
    fireEvent.change(screen.getByLabelText('Mission goal'), { target: { value: 'Ship reduced candidate' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add Input card' }))

    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Mission create failed')
    expect(screen.getByRole('dialog', { name: 'Add Input card' })).toBeInTheDocument()
    expect(screen.getByLabelText('Input card title')).toHaveValue('Plan release')
    expect(screen.getByLabelText('Mission goal')).toHaveValue('Ship reduced candidate')
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
    fireEvent.change(within(dialog).getByLabelText('Workspace'), { target: { value: 'existing' } })
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
    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    expect(within(mission).getByText('Build the page')).toBeInTheDocument()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    expect(within(frame).getByText('Step 1 · Formation · orchestrated')).toBeInTheDocument()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    expect(within(review).getByText('Review the frame')).toBeInTheDocument()
    expect(Number(review.style.zIndex)).toBeGreaterThan(Number(mission.style.zIndex))

    // Clicking the mission card again raises its open window instead of opening another.
    clickCard(screen.getByTestId('mission-node-mis_showcase'))
    await waitFor(() => expect(Number(mission.style.zIndex)).toBeGreaterThan(Number(review.style.zIndex)))
    expect(screen.getAllByRole('dialog', { name: 'Input card · Showcase' })).toHaveLength(1)
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

  it('states routes in words, follows them to other windows, and says a route not wired yet leads nowhere', async () => {
    await renderCockpit()
    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    const routes = within(within(review).getByRole('region', { name: 'Connections' })).getAllByRole('listitem').map(item => item.textContent)
    expect(routes).toEqual(['Fed by 1 Frame', 'Judged by Judge', 'Pass → leads nowhere: wire it to a step or an End node', 'Fail → leads nowhere: wire it to a step or an End node'])
    fireEvent.click(within(review).getByRole('button', { name: 'Judged by Judge' }))
    const judge = await screen.findByRole('dialog', { name: 'Formation · Judge' })
    expect(within(judge).getByText('Judge of 2 Review · solo')).toBeInTheDocument()
    expect(within(judge).getByRole('button', { name: 'Judges 2 Review' })).toBeInTheDocument()
    fireEvent.click(within(review).getByRole('button', { name: 'Fed by 1 Frame' }))
    const frame = await screen.findByRole('dialog', { name: 'Formation · Frame' })
    expect(within(frame).getByRole('button', { name: 'Fed by Showcase' })).toBeInTheDocument()
    expect(within(frame).getByRole('button', { name: 'Feeds → 2 Review' })).toBeInTheDocument()
  })

  it('reads a Limit card on its card, its window, the step it covers and Flow, edits its rounds with undo, and shows a refusal in place', async () => {
    const limited = { ...makeBoard(), limits: [{ id: 'lim_cap', title: 'Cap', target: 'fmn_frame', rounds: 3 }, { id: 'lim_all', title: 'Budget', target: 'mis_showcase', rounds: 20 }] }
    patches = installFetchMock({ boards: [limited] })
    const { unmount } = await renderCockpit()
    const card = screen.getByTestId('limit-node-lim_cap')
    expect(card).toHaveTextContent('Cap')
    expect(screen.getByTestId('limit-knob-lim_cap')).toHaveTextContent('at most 3 rounds')
    expect(screen.getByTestId('limit-covers-lim_cap')).toHaveTextContent('Covers Frame')
    expect(screen.getByTestId('limit-covers-lim_all')).toHaveTextContent('Covers the mission')
    expect(screen.getByTestId('limit-knob-lim_all')).toHaveTextContent('at most 20 step runs')

    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    expect(within(frame).getByRole('button', { name: 'Limit Cap: at most 3 rounds' })).toBeInTheDocument()
    const showcase = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    expect(within(showcase).getByRole('button', { name: 'Limit Budget: the whole mission may make at most 20 step runs' })).toBeInTheDocument()

    const window = await openNodeWindow(card, 'Limit card · Cap')
    expect(within(window).getByLabelText('Covers')).toHaveValue('fmn_frame')
    expect(within(window).getByRole('option', { name: 'Input card — the whole mission' })).toBeInTheDocument()
    expect(within(window).getByTestId('limit-meaning-lim_cap')).toHaveTextContent('Frame may run at most 3 times, send-backs and resumed re-runs included.')
    expect(within(window).getByRole('button', { name: 'Covers 1 Frame' })).toBeInTheDocument()

    // A typed value that is not a positive whole number never leaves the window.
    fireEvent.click(within(window).getByRole('button', { name: 'Edit rounds' }))
    fireEvent.change(within(window).getByRole('textbox', { name: 'Rounds' }), { target: { value: '-2' } })
    fireEvent.click(within(window).getByRole('button', { name: 'Save rounds' }))
    expect(within(window).getByRole('alert')).toHaveTextContent('Enter a positive whole number of rounds, or leave it blank for no limit.')
    expect(patches.filter(patch => patch.body.updateLimit)).toEqual([])

    fireEvent.change(within(window).getByRole('textbox', { name: 'Rounds' }), { target: { value: '5' } })
    fireEvent.click(within(window).getByRole('button', { name: 'Save rounds' }))
    await waitFor(() => expect(screen.getByTestId('limit-knob-lim_cap')).toHaveTextContent('at most 5 rounds'))
    expect(patches.find(patch => patch.body.updateLimit)?.body.updateLimit).toEqual({ id: 'lim_cap', rounds: 5 })

    // One undo puts the old rounds back.
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(screen.getByTestId('limit-knob-lim_cap')).toHaveTextContent('at most 3 rounds'))
    expect(patches.filter(patch => patch.body.updateLimit).slice(-1)[0]?.body.updateLimit).toEqual({ id: 'lim_cap', rounds: 3 })

    // Covering the whole mission from the window.
    fireEvent.change(within(window).getByLabelText('Covers'), { target: { value: 'mis_showcase' } })
    await waitFor(() => expect(screen.getByTestId('limit-covers-lim_cap')).toHaveTextContent('Covers the mission'))
    expect(patches.filter(patch => patch.body.updateLimit).slice(-1)[0]?.body.updateLimit).toEqual({ id: 'lim_cap', target: 'mis_showcase' })
    // Two cards on the Input card is a finding the window names.
    expect(within(window).getByText('The Input card has another Limit card, Budget: keep one.')).toBeInTheDocument()
    unmount()

    // A refusal from the server reads under the field, not in the error bar.
    patches = installFetchMock({ boards: [limited], limitRefusal: 'rounds must be a positive whole number' })
    await renderCockpit()
    const refused = await openNodeWindow(screen.getByTestId('limit-node-lim_cap'), 'Limit card · Cap')
    fireEvent.click(within(refused).getByRole('button', { name: 'Edit rounds' }))
    fireEvent.change(within(refused).getByRole('textbox', { name: 'Rounds' }), { target: { value: '4' } })
    fireEvent.click(within(refused).getByRole('button', { name: 'Save rounds' }))
    await waitFor(() => expect(within(refused).getByRole('alert')).toHaveTextContent('rounds must be a positive whole number'))
    expect(screen.queryByTestId('formations-error')).toBeNull()

    // Flow states each limit on what it covers.
    fireEvent.click(screen.getByRole('radio', { name: 'Flow' }))
    const flow = await screen.findByTestId('flow-view')
    expect(within(flow).getByTestId('flow-step-fmn_frame')).toHaveTextContent('LimitCap: at most 3 rounds')
    expect(within(flow).getByRole('region', { name: 'Input card Showcase' })).toHaveTextContent('LimitBudget: the whole mission may make at most 20 step runs')
  })

  it('sets a Limit card\'s time and warning in its window, each with one undo, and states them on the card, the step and Flow', async () => {
    const limited = { ...makeBoard(), limits: [{ id: 'lim_cap', title: 'Cap', target: 'fmn_frame', rounds: 3 }, { id: 'lim_all', title: 'Budget', target: 'mis_showcase', seconds: 7200 }] }
    patches = installFetchMock({ boards: [limited] })
    await renderCockpit()
    expect(screen.getByTestId('limit-knob-lim_all')).toHaveTextContent('at most 2 h of work')
    const updates = () => patches.filter(patch => patch.body.updateLimit).map(patch => patch.body.updateLimit)
    const window = await openNodeWindow(screen.getByTestId('limit-node-lim_cap'), 'Limit card · Cap')
    expect(within(window).getByText('No time set')).toBeInTheDocument()

    // A time that is not whole seconds never leaves the window.
    fireEvent.click(within(window).getByRole('button', { name: 'Edit time' }))
    for (const value of ['0', '-5m', '1.5s', 'soon']) {
      fireEvent.change(within(window).getByRole('textbox', { name: 'Time' }), { target: { value } })
      fireEvent.click(within(window).getByRole('button', { name: 'Save time' }))
      expect(within(window).getByRole('alert')).toHaveTextContent('Enter a time such as 45s, 30m or 1h30m (whole seconds), or leave it blank for no time limit.')
    }
    expect(updates()).toEqual([])
    fireEvent.change(within(window).getByRole('textbox', { name: 'Time' }), { target: { value: '30m' } })
    fireEvent.click(within(window).getByRole('button', { name: 'Save time' }))
    await waitFor(() => expect(screen.getByTestId('limit-knob-lim_cap')).toHaveTextContent('at most 3 rounds · 30 min'))
    expect(updates()).toEqual([{ id: 'lim_cap', seconds: 1800 }])
    expect(within(window).getByText('30 min of work')).toBeInTheDocument()

    // A warning must be shorter than the time.
    fireEvent.click(within(window).getByRole('button', { name: 'Edit warning' }))
    fireEvent.change(within(window).getByRole('textbox', { name: 'Warning' }), { target: { value: '30m' } })
    fireEvent.click(within(window).getByRole('button', { name: 'Save warning' }))
    expect(within(window).getByRole('alert')).toHaveTextContent('Warn with less time left than the card\'s 30 min.')
    fireEvent.change(within(window).getByRole('textbox', { name: 'Warning' }), { target: { value: '5m' } })
    fireEvent.click(within(window).getByRole('button', { name: 'Save warning' }))
    await waitFor(() => expect(screen.getByTestId('limit-warn-lim_cap')).toHaveTextContent('warns at 5 min left'))
    expect(updates().slice(-1)[0]).toEqual({ id: 'lim_cap', warnSeconds: 300 })
    expect(within(window).getByTestId('limit-meaning-lim_cap')).toHaveTextContent('Waiting on a human gate does not count, so a send-back resumes it with the time it has left.')

    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    expect(within(frame).getByRole('button', { name: 'Limit Cap: at most 3 rounds and 30 min of work, warns at 5 min left' })).toBeInTheDocument()

    // Each change has its own undo entry, restoring the value before it (0 clears).
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(screen.queryByTestId('limit-warn-lim_cap')).toBeNull())
    expect(updates().slice(-1)[0]).toEqual({ id: 'lim_cap', warnSeconds: 0 })
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(screen.getByTestId('limit-knob-lim_cap')).toHaveTextContent(/^at most 3 rounds$/))
    expect(updates().slice(-1)[0]).toEqual({ id: 'lim_cap', seconds: 0 })

    fireEvent.click(screen.getByRole('radio', { name: 'Flow' }))
    const flow = await screen.findByTestId('flow-view')
    expect(within(flow).getByRole('region', { name: 'Input card Showcase' })).toHaveTextContent('LimitBudget: the whole mission may work at most 2 h')
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

  it('edits an Input card goal in its window, undoes it, and the change survives reload', async () => {
    const { unmount } = await renderCockpit()
    const win = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    // The Input card has no Bead; a run names its own.
    expect(within(win).queryByRole('button', { name: 'Edit bead' })).toBeNull()

    fireEvent.click(within(win).getByRole('button', { name: 'Edit goal' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Goal' }), { target: { value: 'Draft a framing for review' } })
    fireEvent.keyDown(within(win).getByRole('textbox', { name: 'Goal' }), { key: 'Enter', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', goal: 'Draft a framing for review' })
    })
    await waitFor(() => expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Draft a framing for review'))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', goal: 'Build the page' })
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
    const win = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    fireEvent.click(within(win).getByRole('button', { name: 'Edit input hint' }))
    fireEvent.change(within(win).getByRole('textbox', { name: 'Input hint' }), { target: { value: 'Paste the page sketch and its copy deck.' } })
    fireEvent.click(within(win).getByRole('button', { name: 'Save input hint' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateInputCard)?.body.updateInputCard).toEqual({ id: 'mis_showcase', inputHint: 'Paste the page sketch and its copy deck.' }))
    expect(await within(win).findByText('Paste the page sketch and its copy deck.')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const start = await screen.findByRole('dialog', { name: 'Start mission' })
    expect(within(start).getByText('Paste the page sketch and its copy deck.')).toBeInTheDocument()
    fireEvent.click(within(start).getByRole('button', { name: 'Cancel' }))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', inputHint: '' }))
  })

  it('switches a mission to talk with the agents from its window, shows it on the card, and undoes it', async () => {
    await renderCockpit()
    const card = screen.getByTestId('mission-node-mis_showcase')
    expect(card).toHaveTextContent('Human gates · Notify me')
    const win = await openNodeWindow(card, 'Input card · Showcase')
    const channel = within(win).getByRole('radiogroup', { name: 'Human gates' })
    expect(within(channel).getByRole('radio', { name: /Notify me/ })).toBeChecked()
    expect(channel).toHaveAccessibleDescription('A change applies to runs started afterwards; runs already going keep their channel.')

    fireEvent.click(within(channel).getByRole('radio', { name: /Talk with the agents/ }))
    await waitFor(() => expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', humanChannel: 'session' }))
    await waitFor(() => expect(card).toHaveTextContent('Human gates · Talk with the agents'))
    expect(within(channel).getByRole('radio', { name: /Talk with the agents/ })).toBeChecked()

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', humanChannel: '' }))
    await waitFor(() => expect(card).toHaveTextContent('Human gates · Notify me'))
    expect(within(channel).getByRole('radio', { name: /Notify me/ })).toBeChecked()
  })

  it('saves a human channel changed in Start mission before the run starts, with undo', async () => {
    patches = installFetchMock({ runStatus: { status: 'succeeded', final: true }, runEvents: [] })
    await renderCockpit()
    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const dialog = await screen.findByRole('dialog', { name: 'Start mission' })
    fireEvent.change(within(dialog).getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(within(dialog).getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Implement the requested change' } })
    fireEvent.click(within(dialog).getByRole('radio', { name: /Talk with the agents/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Start mission' })).toBeNull())
    const calls = vi.mocked(fetch).mock.calls
    const saved = calls.findIndex(([, init]) => typeof init?.body === 'string' && init.body.includes('"humanChannel":"session"'))
    const started = calls.findIndex(([url, init]) => url === '/api/runs' && init?.method === 'POST')
    expect(saved).toBeGreaterThanOrEqual(0)
    expect(started).toBeGreaterThan(saved)
    expect(screen.getByTestId('mission-node-mis_showcase')).toHaveTextContent('Human gates · Talk with the agents')

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', humanChannel: '' }))

    // An unchanged channel saves nothing before starting.
    const before = patches.length
    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const again = await screen.findByRole('dialog', { name: 'Start mission' })
    expect(within(again).getByRole('radio', { name: /Notify me/ })).toBeChecked()
    fireEvent.change(within(again).getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(within(again).getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(within(again).getByLabelText('Brief'), { target: { value: 'Again' } })
    fireEvent.click(within(again).getByRole('button', { name: 'Start mission' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Start mission' })).toBeNull())
    expect(patches.slice(before).filter(patch => patch.body.updateInputCard)).toEqual([])
  })

  it('edits the reference files of a mission and a gate in their windows, with undo', async () => {
    await renderCockpit()
    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    fireEvent.click(within(mission).getByRole('button', { name: 'Edit files' }))
    fireEvent.change(within(mission).getByRole('textbox', { name: 'Files' }), { target: { value: 'docs/sketch.md, docs/copy.md' } })
    fireEvent.click(within(mission).getByRole('button', { name: 'Save files' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateInputCard)?.body.updateInputCard).toEqual({ id: 'mis_showcase', files: ['docs/sketch.md', 'docs/copy.md'] }))
    expect(await within(mission).findByRole('button', { name: 'Open file docs/copy.md' })).toBeInTheDocument()

    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    fireEvent.click(within(review).getByRole('button', { name: 'Edit files' }))
    fireEvent.change(within(review).getByRole('textbox', { name: 'Files' }), { target: { value: 'rubrics/review.md' } })
    fireEvent.click(within(review).getByRole('button', { name: 'Save files' }))
    await waitFor(() => expect(patches.find(patch => patch.body.updateGate)?.body.updateGate).toEqual({ id: 'gate_review', files: ['rubrics/review.md'] }))

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateGate).slice(-1)[0]?.body.updateGate).toEqual({ id: 'gate_review', files: [] }))
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateInputCard).slice(-1)[0]?.body.updateInputCard).toEqual({ id: 'mis_showcase', files: [] }))
  })

  it('opens the node thread and referenced files from a node window without copying them', async () => {
    const referenced = makeBoard()
    referenced.formations = [{ ...formation, brief: { goal: 'Frame it', files: ['docs/sketch.md'] } }, judgeFormation] as TestBoard['formations']
    patches = installFetchMock({
      boards: [referenced],
      boardNotes: { elements: [{ nodeId: 'fmn_frame', entries: [noteEntry('nte_1', 'human:operator', 'Keep the frame narrow')] }] },
    })
    await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    const notes = within(frame).getByRole('region', { name: 'Notes' })
    expect(notes).toHaveTextContent('1 entry in the thread')
    expect(within(frame).queryByText('Keep the frame narrow')).toBeNull()
    fireEvent.click(within(notes).getByRole('button', { name: 'Open notes' }))
    const thread = await screen.findByRole('dialog', { name: 'notes for Frame' })
    expect(thread).toHaveTextContent('Keep the frame narrow')

    fireEvent.click(within(frame).getByRole('button', { name: 'Open file docs/sketch.md' }))
    const file = await screen.findByRole('dialog', { name: 'file sketch.md' })
    expect(file).toHaveTextContent('Frame · docs/sketch.md')

    const review = await openNodeWindow(screen.getByTestId('gate-node-gate_review'), 'Gate · Review')
    fireEvent.click(within(within(review).getByRole('region', { name: 'Notes' })).getByRole('button', { name: 'Add a note' }))
    expect(await screen.findByRole('dialog', { name: 'notes for Review' })).toBeInTheDocument()
    expect(patches).toEqual([])
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

  it('states a vanilla slot as staffed with no role, and restores a slot\'s own model and effort on undo', async () => {
    const board = makeBoard()
    board.formations = board.formations.map(item => item.id === 'fmn_frame'
      ? { ...item, slots: [{ id: 'slot_lead', label: 'Lead', controller: true, agentId: 'mason', harness: 'codex', model: 'gpt-6-astra', effort: 'xhigh' }, { id: 'slot_worker', label: 'Worker', controller: false, harness: 'claude', model: 'opus', effort: 'low' }] }
      : item) as typeof board.formations
    patches = installFetchMock({ boards: [board] })
    await renderCockpit()
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    const staffing = within(frame).getByRole('region', { name: 'Staffing' })
    expect(await within(staffing).findByText('Worker is a vanilla agent on claude, model opus, low effort.')).toBeInTheDocument()
    expect(within(staffing).getByRole('combobox', { name: 'Persona for Worker' })).toHaveDisplayValue('No role (vanilla)')
    expect(within(staffing).getByText('Lead (controller) is Mason (mason) on codex, model gpt-6-astra, xhigh effort.')).toBeInTheDocument()

    fireEvent.change(within(staffing).getByRole('combobox', { name: 'Persona for Lead' }), { target: { value: 'hazel' } })
    await waitFor(() => expect(patches.find(patch => patch.body.assignSlot)).toBeTruthy())
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.filter(patch => patch.body.assignSlot).slice(-1)[0]?.body.assignSlot).toEqual({ formationId: 'fmn_frame', slotId: 'slot_lead', agentId: 'mason', harness: 'codex', model: 'gpt-6-astra', effort: 'xhigh' })
    })
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
      expect(patches.filter(patch => patch.body.assignSlot).slice(-1)[0]?.body.assignSlot).toEqual({ formationId: 'fmn_frame', slotId: 'slot_lead', agentId: 'mason', harness: 'codex', model: 'mason-model', effort: 'medium' })
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
    expect(within(create).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Solo formation', 'Peer formation', 'Orchestrated formation', 'Gate', 'End node · done', 'End node · rejected', 'Limit card'])
  })

  it('offers one solo choice per staffed slot so no agent is dropped silently', async () => {
    const staffedBoard = makeBoard()
    staffedBoard.formations = [{
      ...formation,
      slots: [
        { id: 'slot_lead', label: 'Lead', controller: true, agentId: 'mason', harness: 'codex', model: 'mason-model', effort: 'medium' },
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

  it('undoes a deleted gate with its wires instead of an earlier rename, then the rename', async () => {
    await renderCockpit()
    const judge = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_judge')).getAllByText('Judge')[0], 'Formation · Judge')
    fireEvent.click(within(judge).getByRole('button', { name: 'Edit title' }))
    fireEvent.change(within(judge).getByRole('textbox', { name: 'Title' }), { target: { value: 'Review' } })
    fireEvent.click(within(judge).getByRole('button', { name: 'Save title' }))
    await waitFor(() => expect(patches.filter(patch => patch.body.updateFormation)).toHaveLength(1))

    fireEvent.contextMenu(screen.getByTestId('gate-node-gate_review'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete gate' }))
    await waitFor(() => expect(screen.queryByTestId('gate-node-gate_review')).toBeNull())

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    expect(await screen.findByTestId('gate-node-gate_review')).toBeInTheDocument()
    const restore = patches.find(patch => patch.body.restoreNode)?.body.restoreNode as { gate: TestGate; connections: Array<{ id: string }>; x: number; y: number }
    expect(restore.gate).toEqual(gate)
    expect(restore.connections.map(edge => edge.id).sort()).toEqual(['edge_frame_gate', 'edge_judge_return', 'edge_judge_send'])
    expect(restore).toEqual(expect.objectContaining({ x: 860, y: 100 }))
    expect(patches.filter(patch => patch.body.updateFormation)).toHaveLength(1)
    expect(screen.queryByTestId('formations-error')).toBeNull()

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.filter(patch => patch.body.updateFormation).map(patch => patch.body.updateFormation)).toEqual([
      { id: 'fmn_judge', title: 'Review' },
      { id: 'fmn_judge', title: 'Judge' },
    ]))
  })

  it('restores a deleted formation and mission with their staffing, ports and brief', async () => {
    await renderCockpit()
    for (const [testId, item, op] of [['formation-node-fmn_judge', 'Delete formation', 'formation'], ['mission-node-mis_showcase', 'Delete Input card', 'inputCard']] as const) {
      fireEvent.contextMenu(screen.getByTestId(testId))
      fireEvent.click(await screen.findByRole('menuitem', { name: item }))
      await waitFor(() => expect(screen.queryByTestId(testId)).toBeNull())
      fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
      expect(await screen.findByTestId(testId)).toBeInTheDocument()
      const restored = patches.filter(patch => patch.body.restoreNode).slice(-1)[0].body.restoreNode as Record<string, unknown>
      expect(restored[op]).toEqual(op === 'formation' ? judgeFormation : mission)
    }
    expect(screen.queryByTestId('formations-error')).toBeNull()
  })

  it('reports a failed undo once, drops it, and the next Ctrl+Z reaches older history', async () => {
    patches = installFetchMock({ restoreFailure: true })
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_judge'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Add output port' }))
    await waitFor(() => expect(patches.filter(patch => patch.body.addPort)).toHaveLength(1))
    fireEvent.contextMenu(screen.getByTestId('mission-node-mis_showcase'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete Input card' }))
    await waitFor(() => expect(screen.queryByTestId('mission-node-mis_showcase')).toBeNull())

    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    expect(await screen.findByTestId('formations-error')).toHaveTextContent(
      'Could not undo the delete of Input card “Showcase”: node "mis_showcase" is already in the mission. It was removed from the undo history.')
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(patches.map(patch => patch.body.removePort).filter(Boolean)).toEqual([{ formationId: 'fmn_judge', portId: 'port_added_7' }]))
    expect(patches.filter(patch => patch.body.restoreNode)).toHaveLength(1)
    await waitFor(() => expect(screen.queryByTestId('formations-error')).toBeNull())
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(patches.filter(patch => patch.body.restoreNode || patch.body.removePort)).toHaveLength(2)
  })

  it('reloads and retries once when another editor changed the board, keeping the undo', async () => {
    patches = installFetchMock({ conflictOnce: 'restoreNode' })
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('mission-node-mis_showcase'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete Input card' }))
    await waitFor(() => expect(screen.queryByTestId('mission-node-mis_showcase')).toBeNull())
    const reads = () => recordedFetches.filter(url => url.endsWith('/missions/test-board')).length
    const readsBefore = reads()
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    expect(await screen.findByTestId('mission-node-mis_showcase')).toBeInTheDocument()
    expect(patches.filter(patch => patch.body.restoreNode)).toHaveLength(2)
    expect(reads()).toBeGreaterThan(readsBefore)
    expect(screen.queryByTestId('formations-error')).toBeNull()
  })

  it('waits for an edit in flight, so Ctrl+Z undoes that edit and not an older one', async () => {
    let release: () => void = () => undefined
    patches = installFetchMock({ addPortGate: new Promise<void>(resolve => { release = resolve }) })
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('mission-node-mis_showcase'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete Input card' }))
    await waitFor(() => expect(screen.queryByTestId('mission-node-mis_showcase')).toBeNull())
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_judge'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Add output port' }))
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(patches.filter(patch => patch.body.restoreNode || patch.body.removePort)).toEqual([])
    release()
    await waitFor(() => expect(patches.map(patch => patch.body.removePort).filter(Boolean)).toHaveLength(1))
    expect(patches.filter(patch => patch.body.restoreNode)).toEqual([])
    expect(screen.queryByTestId('mission-node-mis_showcase')).toBeNull()
    expect(screen.queryByTestId('formations-error')).toBeNull()
  })

  it('undoes Remove this input by restoring the port in place with its wire', async () => {
    await renderCockpit()
    fireEvent.contextMenu(document.querySelector<HTMLElement>('[data-port-in="fmn_judge:port_judge_in"]')!.closest<HTMLElement>('.fio')!)
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove this input' }))
    await waitFor(() => expect(document.querySelector('[data-port-in="fmn_judge:port_judge_in"]')).toBeNull())
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(document.querySelector('[data-port-in="fmn_judge:port_judge_in"]')).not.toBeNull())
    expect(patches.find(patch => patch.body.restorePort)?.body.restorePort).toEqual({
      formationId: 'fmn_judge', direction: 'input', port: { id: 'port_judge_in', label: 'Input' }, index: 0,
      connections: [{ id: 'edge_judge_send', from: 'gate_review:judge', to: 'fmn_judge:port_judge_in' }],
    })
  })

  it('records one undo for a toolbar New formation', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByTestId('new-formation'))
    expect(await screen.findByTestId('formation-node-fmn_created')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => expect(screen.queryByTestId('formation-node-fmn_created')).toBeNull())
    expect(patches.map(patch => patch.body.deleteFormation).filter(Boolean)).toEqual([{ id: 'fmn_created' }])
  })

  it('adds an input port from the formation context menu, and one undo removes it', async () => {
    await renderCockpit()
    fireEvent.contextMenu(screen.getByTestId('formation-node-fmn_frame'))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Add input port' }))
    await waitFor(() => {
      const port = patches.map(patch => patch.body.addPort as { direction?: string } | undefined).find(Boolean)
      expect(port).toEqual(expect.objectContaining({ formationId: 'fmn_frame', direction: 'input', label: 'Input' }))
    })
    await waitFor(() => expect(screen.queryByTestId('formations-error')).toBeNull())
    fireEvent.keyDown(window, { key: 'z', ctrlKey: true })
    await waitFor(() => {
      expect(patches.map(patch => patch.body.removePort).filter(Boolean)).toEqual([{ formationId: 'fmn_frame', portId: 'port_added_7' }])
    })
  })

  it('adds ports in the server vocabulary from the card, input row and output row menus', async () => {
    await renderCockpit()
    const menuAdd = async (target: HTMLElement, name: string) => {
      fireEvent.contextMenu(target)
      fireEvent.click(await screen.findByRole('menuitem', { name }))
    }
    await menuAdd(screen.getByTestId('formation-node-fmn_frame'), 'Add output port')
    await waitFor(() => expect(patches.filter(patch => patch.body.addPort)).toHaveLength(1))
    await menuAdd(document.querySelector<HTMLElement>('[data-port-in="fmn_frame:port_frame_in"]')!.closest<HTMLElement>('.fio')!, 'Add input port')
    await waitFor(() => expect(patches.filter(patch => patch.body.addPort)).toHaveLength(2))
    await menuAdd(document.querySelector<HTMLElement>('[data-port-out="fmn_frame:port_frame_out"]')!.closest<HTMLElement>('.fio')!, 'Add output port')
    await waitFor(() => expect(patches.filter(patch => patch.body.addPort)).toHaveLength(3))
    expect(patches.map(patch => (patch.body.addPort as { direction?: string } | undefined)?.direction).filter(Boolean)).toEqual(['output', 'input', 'output'])
    expect(screen.queryByTestId('formations-error')).toBeNull()
  })

  it('loads final evidence when a lab mission completes before its start response is read', async () => {
    patches = installFetchMock({
      runStatus: { status: 'succeeded', final: true },
      runEvents: [{runId:'run_legacy',seq:1,type:'node_output',nodeId:'fmn_frame',status:'done'}],
    })
    await renderCockpit()
    fireEvent.click(screen.getByTestId('run-mission-mis_showcase'))
    const dialog = await screen.findByRole('dialog', { name: 'Start mission' })
    fireEvent.change(within(dialog).getByLabelText('Workspace'), { target: { value: 'existing' } })
    fireEvent.change(within(dialog).getByLabelText('Working directory'), { target: { value: '/work/project' } })
    fireEvent.change(within(dialog).getByLabelText('Brief'), { target: { value: 'Implement the requested change' } })
    fireEvent.change(within(dialog).getByLabelText('Bead'), { target: { value: 'form-proof' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Start mission' }))
    await screen.findByTestId('inspect-node-fmn_frame')
    expect(fetch).toHaveBeenCalledWith('/api/runs', expect.objectContaining({ body: expect.stringContaining('"cwd":"/work/project"') }))
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => url === '/api/runs' && init?.method === 'POST')
    const body = JSON.parse(String(call?.[1]?.body))
    expect(body).toMatchObject({ cwd: '/work/project', brief: 'Implement the requested change', beadId: 'form-proof' })
    // The run starts without limits (archon-o7p.7).
    expect(body).not.toHaveProperty('limits')
    expect(localStorage.getItem('archon.activeRun.test-board')).toBeNull()
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

    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
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
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
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
      const data = url === '/api/runs/run_legacy/evidence/nodes/fmn_frame' ? { evidence }
        : url === '/api/runs/run_legacy/evidence/artifacts' ? { artifacts: [], truncated: false } : null
      if (!data) return projectionFetch(input, init)
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data }) } as unknown as Response)
    }) as typeof fetch
    await renderCockpit()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/runs/run_legacy/events', expect.anything()))
    fireEvent.click(await screen.findByTestId('inspect-node-fmn_frame'))
    const dialog = await screen.findByTestId('node-inspector')
    expect(within(dialog).getByTestId('node-evidence-state')).toHaveTextContent('done')
    expect(await within(dialog).findByTestId('node-output-value')).toHaveTextContent('Framed the problem')
    expect(dialog).toHaveTextContent('slot_lead')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close run evidence' }))
    await waitFor(() => expect(screen.queryByTestId('node-inspector')).toBeNull())
  })

  it('surfaces open escalations as a needs-you banner and marks the escalated node', async () => {
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
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
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/runs/run_legacy/escalations', expect.anything()))

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
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
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

    const mission = await openNodeWindow(screen.getByTestId('mission-node-mis_showcase'), 'Input card · Showcase')
    expect(within(mission).queryByRole('region', { name: 'Run' })).toBeNull()
  })

  type ListedRun = { runId: string; status: string; final: boolean; missionSlug: string; inputCardId: string; eventCount: number; waitingGates?: Array<{ gateId: string; requestedSeq: number }> }
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
      const listed = url.match(/^\/api\/runs\?mission=([^&]+)$/)
      if (listed) return reply(runs.filter(run => run.missionSlug === decodeURIComponent(listed[1])))
      const runURL = url.match(/^\/api\/runs\/([^/?]+)(\/.*)?$/)
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
    const runs = [{ runId: 'run_01CLI', status: 'waiting_human', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] }]
    const verdicts = installRunsMock(runs, { run_01CLI: waitingEvents('run_01CLI') })
    expect(localStorage.length).toBe(0)
    await renderCockpit()

    expect(await screen.findByTestId('run-banner')).toHaveTextContent('Waiting for your answer')
    const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    await waitFor(() => expect(within(panel).getByText('Question for run_01CLI')).toBeInTheDocument())
    expect(fetch).toHaveBeenCalledWith('/api/runs?mission=test-board', expect.anything())
    expect(screen.queryByRole('combobox', { name: 'Choose run' })).toBeNull()

    fireEvent.change(within(panel).getByLabelText('Your response'), { target: { value: 'Postgres' } })
    await act(async () => { fireEvent.click(within(panel).getByRole('button', { name: 'Approve' })) })
    await waitFor(() => expect(verdicts).toEqual([{ url: '/api/runs/run_01CLI/gates/gate_review/verdict', body: { actor: 'agent:ui', verdict: 'pass', requestedSeq: 4, reason: 'Postgres' } }]))
  })

  it('opens truncated gate input evidence for the selected run without submitting the draft', async () => {
    installRunsMock([{ runId: 'run_01EVIDENCE', status: 'waiting_human', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] }], { run_01EVIDENCE: waitingEvents('run_01EVIDENCE') })
    const fallback = vi.mocked(fetch).getMockImplementation()!
    const reply = (data: unknown) => new Response(JSON.stringify({ success: true, data }), { status: 200 })
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/runs/run_01EVIDENCE/gates/gate_review/request') return reply({ request: { gateId: 'gate_review', requestedSeq: 4, criterion: 'Review', input: { fromNodeId: 'fmn_frame', text: 'Part of the input', truncated: true } } })
      return fallback(input, init)
    })
    await renderCockpit()
    const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    fireEvent.change(within(panel).getByLabelText('Your response'), { target: { value: 'Keep this draft' } })
    fireEvent.click(await within(panel).findByRole('button', { name: 'Open run evidence' }))
    expect(await screen.findByRole('dialog', { name: 'Run evidence · Review' })).toBeInTheDocument()
    await waitFor(() => expect(fetch).toHaveBeenCalledWith('/api/runs/run_01EVIDENCE/evidence/nodes/gate_review', expect.anything()))
    expect(within(panel).getByLabelText('Your response')).toHaveValue('Keep this draft')
    expect(screen.getByTestId('run-banner').querySelector('.badge')).toHaveClass('waiting_human')
  })

  it('shows the open run that needs the operator and offers a run picker', async () => {
    installRunsMock([
      { runId: 'run_01A', status: 'waiting_human', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
      { runId: 'run_01B', status: 'running', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 2 },
      { runId: 'run_01C', status: 'succeeded', final: true, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 9 },
    ], { run_01A: waitingEvents('run_01A') })
    await renderCockpit()

    const picker = await screen.findByRole('combobox', { name: 'Choose run' })
    expect(picker).toHaveValue('run_01A')
    expect(within(picker).getAllByRole('group').map(group => [group.getAttribute('label'), within(group).getAllByRole('option').map(option => option.getAttribute('value'))])).toEqual([
      ['Open', ['run_01A', 'run_01B']],
      ['Finished', ['run_01C']],
    ])
    expect(await screen.findByRole('dialog', { name: 'Answer gate Review' })).toBeInTheDocument()

    fireEvent.change(picker, { target: { value: 'run_01B' } })
    await waitFor(() => expect(screen.getByTestId('run-banner')).toHaveTextContent('Running'))
    expect(screen.queryByRole('dialog', { name: 'Answer gate Review' })).toBeNull()
    expect(window.location.search).toBe('?mission=test-board&run=run_01B')
  })

  it('opens a board and run from a link and keeps them on reload', async () => {
    window.history.replaceState(null, '', '/?mission=second-board&run=run_01LINK')
    const second = { ...makeBoard(), id: 'brd_second', slug: 'second-board', title: 'Second board', etag: 'second-etag' }
    patches = installFetchMock({ boards: [makeBoard(), second] })
    installRunsMock([
      { runId: 'run_01LINK', status: 'waiting_human', final: false, missionSlug: 'second-board', inputCardId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
      { runId: 'run_01NEWER', status: 'waiting_human', final: false, missionSlug: 'second-board', inputCardId: 'mis_showcase', eventCount: 4, waitingGates: [{ gateId: 'gate_review', requestedSeq: 4 }] },
    ], { run_01LINK: waitingEvents('run_01LINK'), run_01NEWER: waitingEvents('run_01NEWER') })

    for (let load = 0; load < 2; load++) {
      const { unmount } = await renderCockpit()
      await waitFor(() => expect(screen.getByTestId('board-picker')).toHaveValue('second-board'))
      expect(await screen.findByRole('combobox', { name: 'Choose run' })).toHaveValue('run_01LINK')
      const panel = await screen.findByRole('dialog', { name: 'Answer gate Review' })
      await waitFor(() => expect(within(panel).getByText('Question for run_01LINK')).toBeInTheDocument())
      expect(window.location.search).toBe('?mission=second-board&run=run_01LINK')
      unmount()
    }
  })

  it('says when a linked run or board does not exist', async () => {
    window.history.replaceState(null, '', '/?mission=test-board&run=run_01GONE')
    installRunsMock([{ runId: 'run_01OPEN', status: 'running', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 2 }])
    const { unmount } = await renderCockpit()
    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Run run_01GONE from the link was not found')
    await waitFor(() => expect(screen.getByTestId('run-banner')).toHaveTextContent('Running'))
    expect(window.location.search).toBe('?mission=test-board')
    unmount()

    window.history.replaceState(null, '', '/?mission=no-such-board&run=run_01OPEN')
    await renderCockpit()
    expect(await screen.findByTestId('formations-error')).toHaveTextContent('Mission "no-such-board" from the link was not found')
    expect(screen.getByTestId('board-picker')).toHaveValue('test-board')
  })

  it('names where a blocked run stopped, rings that gate and locates it from the run bar', async () => {
    installRunsMock([{ runId: 'run_01BLOCK', status: 'blocked', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 3 }], {
      run_01BLOCK: [
        { runId: 'run_01BLOCK', seq: 1, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01BLOCK', seq: 2, type: 'judge_attempt_failed', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01BLOCK', seq: 3, type: 'run_blocked', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    const projection = globalThis.fetch
    const problems = [
      { seq: 3, type: 'run_blocked', nodeIds: ['gate_review'], reason: { text: 'invalid judge result: missing verdict block', bytes: 42 }, resumeAllowed: false },
    ]
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => String(input) === '/api/runs/run_01BLOCK/evidence/problems'
      ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data: { problems } }) } as unknown as Response)
      : projection(input, init)) as typeof fetch
    await renderCockpit()

    const point = await screen.findByTestId('run-point')
    await waitFor(() => expect(point).toHaveTextContent('blocked at Review: invalid judge result: missing verdict block'))
    const gate = screen.getByTestId('gate-node-gate_review')
    expect(gate).toHaveClass('blocked')
    expect(within(gate).getByTestId('run-chip-gate_review')).toHaveTextContent('blocked')
    fireEvent.click(point)
    expect(gate).toHaveClass('located')
    // Once the card is centred, its node window opens beside it.
    expect(await screen.findByRole('dialog', { name: 'Gate · Review' })).toBeInTheDocument()
  })

  it('shows what a finished run produced and opens it in a file window', async () => {
    window.history.replaceState(null, '', '/?mission=test-board&run=run_01DONE')
    installRunsMock([{ runId: 'run_01DONE', status: 'succeeded', final: true, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 6 }], {
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
    const output = (nodeId: string, seq: number, port: string, body: string, artifact?: string) => ({ evidence: { runId: 'run_01DONE', nodeId, kind: 'formation', definition: {
      title: nodeId === 'fmn_frame' ? 'Frame' : 'Judge', outputs: [{ id: port, label: 'Output' }], outgoing: makeBoard().connections.filter(edge => edge.from.startsWith(`${nodeId}:`)),
    }, attempts: [
      { attempt: 1, inputs: [], dispatches: [], output: { seq, text: text(body), ports: [{ portId: port, text: text(body), ref: artifact ? { artifact } : undefined }] } },
    ] } })
    const evidence: Record<string, unknown> = {
      '/api/runs/run_01DONE/evidence/nodes/fmn_frame': output('fmn_frame', 2, 'port_frame_out', '# Frame\n\nThe **problem**', 'frame.md'),
      '/api/runs/run_01DONE/evidence/nodes/fmn_judge': output('fmn_judge', 4, 'port_judge_out', 'verdict pass'),
      '/api/runs/run_01DONE/evidence/artifacts': { artifacts: [{ name: 'frame.md', size: 24, modifiedAt: '' }], truncated: false },
      '/api/runs/run_01DONE/evidence/artifacts/frame.md': { artifact: { name: 'frame.md', size: 24, modifiedAt: '', kind: 'markdown', text: text('# Frame\n\nThe **problem**') } },
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

    // The step's node window carries the same chips.
    const frame = await openNodeWindow(within(screen.getByTestId('formation-node-fmn_frame')).getByText('Frame'), 'Formation · Frame')
    expect(within(within(frame).getByRole('region', { name: 'Run' })).getByRole('button', { name: 'frame.md' })).toBeInTheDocument()
  })

  it.each(['deletion', 'rename', 'rewiring'])('reopens historical produced outputs using frozen names and topology after board %s', async edit => {
    const currentBoard = makeBoard()
    if (edit === 'deletion') currentBoard.formations = currentBoard.formations.filter(node => node.id !== 'fmn_frame')
    if (edit === 'rename') currentBoard.formations = currentBoard.formations.map(node => ({
      ...node, title: `Edited ${node.title}`, outputs: node.outputs.map(port => ({ ...port, label: 'Edited output' })),
    }))
    if (edit === 'rewiring') currentBoard.connections = []
    patches = installFetchMock({ boards: [currentBoard] })
    window.history.replaceState(null, '', '/?mission=test-board&run=run_01HISTORY')
    installRunsMock([{ runId: 'run_01HISTORY', status: 'succeeded', final: true, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 5 }], {
      run_01HISTORY: [
        { runId: 'run_01HISTORY', seq: 2, type: 'node_output', nodeId: 'fmn_frame' },
        { runId: 'run_01HISTORY', seq: 4, type: 'node_output', nodeId: 'fmn_judge' },
        { runId: 'run_01HISTORY', seq: 5, type: 'run_succeeded' },
      ],
    })
    const base = globalThis.fetch
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const nodeId = url.split('/evidence/nodes/')[1]
      if (!nodeId && !url.endsWith('/evidence/artifacts')) return base(input, init)
      const original = makeBoard().formations.find(node => node.id === nodeId)
      const text = { text: nodeId === 'fmn_frame' ? 'The recorded deliverable' : 'Recorded judge verdict', bytes: 24 }
      const data = original ? { evidence: {
        runId: 'run_01HISTORY', nodeId, kind: 'formation',
        definition: { title: original.title, outputs: original.outputs, outgoing: makeBoard().connections.filter(edge => edge.from.startsWith(`${nodeId}:`)) },
        attempts: [{ attempt: 1, inputs: [], dispatches: [], output: { seq: nodeId === 'fmn_frame' ? 2 : 4, text, ports: [{ portId: original.outputs[0].id, text }] } }],
      } } : { artifacts: [] }
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: async () => ({ success: true, data }) } as unknown as Response)
    }) as typeof fetch
    render(<FormationsCockpit />)
    await screen.findByTestId('formation-node-fmn_judge')

    const produced = await screen.findByTestId('run-produced')
    await waitFor(() => expect(within(produced).getAllByRole('button').map(button => button.textContent)).toEqual(['¶Output', '+1']))
    const chip = within(produced).getByRole('button', { name: 'Output' })
    expect(chip).toHaveAttribute('title', expect.stringContaining('from Frame'))
    fireEvent.click(chip)
    const report = await screen.findByRole('dialog', { name: 'file Output' })
    expect(await within(report).findByText('The recorded deliverable')).toBeInTheDocument()
    expect(report).toHaveTextContent('Frame')
    if (edit === 'deletion') expect(screen.queryByTestId('formation-node-fmn_frame')).toBeNull()
    if (edit === 'rename') expect(screen.getByTestId('formation-node-fmn_frame')).toHaveTextContent('Edited Frame')
    // Reading history preserves the editable board and issues no authoring mutation.
    expect(patches).toEqual([])
  })

  it('shows referenced files on cards, with a judge\'s brief files on its gate, and opens a rubric in a file window', async () => {
    const withFiles = makeBoard()
    const rubricGate = { ...gate, files: ['/srv/rubrics/review.md', '/srv/rubrics/scale.md'] }
    const briefedJudge = { ...judgeFormation, brief: { goal: 'Judge the frame', files: ['/srv/rubrics/review.md', 'judge.md'] } }
    const missionWithFiles = { ...mission, files: ['docs/sketch.md'] }
    withFiles.gates = [rubricGate]
    withFiles.inputCards = [missionWithFiles]
    withFiles.formations = [formation, briefedJudge]
    patches = installFetchMock({ boards: [withFiles] })
    const coordinator = globalThis.fetch
    const previews: string[] = []
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (!url.startsWith('/api/files/preview')) return coordinator(input, init)
      previews.push(url)
      // The daemon reads any absolute path and has no base for a relative one.
      const relative = !decodeURIComponent(url.split('path=')[1]).startsWith('/')
      return Promise.resolve({
        ok: !relative,
        status: relative ? 400 : 200,
        headers: { get: () => null },
        json: () => Promise.resolve(relative
          ? { success: false, error: { code: 'Bad Request', message: 'a relative file reference has no base here; name the file by its absolute path' } }
          : { success: true, data: { file: { path: '/srv/rubrics/review.md', name: 'review.md', size: 16, modifiedAt: '', kind: 'markdown', text: { text: '# Review rubric', bytes: 15 } } } }),
      } as unknown as Response)
    }) as typeof fetch
    await renderCockpit()

    const gateCard = screen.getByTestId('gate-node-gate_review')
    const gateRefs = within(gateCard).getByRole('group', { name: 'Referenced files' })
    expect(within(gateRefs).getAllByRole('button').map(button => button.getAttribute('aria-label'))).toEqual(['Open /srv/rubrics/review.md', '2 more referenced files'])
    expect(within(screen.getByTestId('mission-node-mis_showcase')).getByRole('button', { name: 'Open docs/sketch.md' })).toHaveTextContent('sketch.md')
    expect(within(screen.getByTestId('refs-fmn_judge')).getAllByRole('button').map(button => button.textContent)).toEqual(['▤review.md', '▤judge.md'])
    expect(screen.queryByTestId('refs-fmn_frame')).toBeNull()

    fireEvent.click(within(gateRefs).getByRole('button', { name: 'Open /srv/rubrics/review.md' }))
    const rubric = await screen.findByRole('dialog', { name: 'file review.md' })
    expect(await within(rubric).findByRole('heading', { name: 'Review rubric' })).toBeInTheDocument()
    expect(rubric).toHaveTextContent('Review · /srv/rubrics/review.md')

    fireEvent.click(within(gateRefs).getByRole('button', { name: '2 more referenced files' }))
    const more = await screen.findByRole('menu', { name: 'Referenced files' })
    expect(within(more).getAllByRole('menuitem').map(item => item.textContent)).toEqual(['/srv/rubrics/scale.md', 'judge.md · judge Judge'])
    fireEvent.click(within(more).getByRole('menuitem', { name: 'judge.md · judge Judge' }))
    const unresolved = await screen.findByRole('dialog', { name: 'file judge.md' })
    expect(await within(unresolved).findByRole('alert')).toHaveTextContent('Cannot read judge.md: a relative file reference has no base here')
    expect(unresolved).toHaveTextContent('Judge (judge) · judge.md')
    expect(previews).toEqual(['/api/files/preview?path=%2Fsrv%2Frubrics%2Freview.md', '/api/files/preview?path=judge.md'])

    // The gate's window lists its own files, then the judge's brief files the gate does not already name.
    const review = await openNodeWindow(within(gateCard).getByText('Review the frame'), 'Gate · Review')
    expect(within(review).getAllByRole('button', { name: /^Open file / }).map(button => button.getAttribute('aria-label')))
      .toEqual(['Open file /srv/rubrics/review.md', 'Open file /srv/rubrics/scale.md', 'Open file judge.md'])
    const judgeFiles = within(review).getByRole('list', { name: "Judge Judge's brief files" })
    fireEvent.click(within(judgeFiles).getByRole('button', { name: 'Open file judge.md' }))
    expect(await screen.findByRole('dialog', { name: 'file judge.md' })).toHaveTextContent('Judge (judge) · judge.md')
    expect(recordedMutations).toEqual([])
  })

  it('reopens a finished run from the run bar and puts it away again', async () => {
    installRunsMock([
      { runId: 'run_01M2A0OLDER', status: 'failed', final: true, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 3 },
      { runId: 'run_01M2B0NEWER', status: 'succeeded', final: true, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 2 },
    ], {
      run_01M2B0NEWER: [
        { runId: 'run_01M2B0NEWER', seq: 1, type: 'node_output', nodeId: 'fmn_frame', status: 'done' },
        { runId: 'run_01M2B0NEWER', seq: 2, type: 'run_succeeded' },
      ],
    })
    const projection = globalThis.fetch
    const text = (value: string) => ({ text: value, bytes: value.length })
    const evidence: Record<string, unknown> = {
      '/api/runs/run_01M2B0NEWER/evidence/nodes/fmn_frame': { evidence: { runId: 'run_01M2B0NEWER', nodeId: 'fmn_frame', kind: 'formation', attempts: [
        { attempt: 1, inputs: [], dispatches: [], output: { seq: 1, text: text('# Frame'), ports: [{ portId: 'port_frame_out', text: text('# Frame'), ref: { artifact: 'frame.md' } }] } },
      ] } },
      '/api/runs/run_01M2B0NEWER/evidence/artifacts': { artifacts: [{ name: 'frame.md', size: 7, modifiedAt: '' }], truncated: false },
    }
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => String(input) in evidence
      ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data: evidence[String(input)] }) } as unknown as Response)
      : projection(input, init)) as typeof fetch
    await renderCockpit()

    const idle = await screen.findByTestId('run-banner-idle')
    expect(idle).toHaveTextContent('no open run')
    expect(screen.queryByTestId('run-banner')).toBeNull()
    const picker = within(idle).getByRole('combobox', { name: 'Choose run' })
    expect(picker).toHaveValue('')
    expect(within(picker).getAllByRole('option').map(option => option.textContent)).toEqual(['Recent runs…', 'Succeeded · …0NEWER', 'Failed · …0OLDER'])

    fireEvent.change(picker, { target: { value: 'run_01M2B0NEWER' } })
    const banner = await screen.findByTestId('run-banner')
    expect(banner).toHaveTextContent('Succeeded')
    expect(window.location.search).toBe('?mission=test-board&run=run_01M2B0NEWER')
    await waitFor(() => expect(within(banner).getByRole('button', { name: 'frame.md' })).toBeInTheDocument())
    const shown = within(banner).getByRole('combobox', { name: 'Choose run' })
    expect(shown).toHaveValue('run_01M2B0NEWER')

    fireEvent.change(shown, { target: { value: '' } })
    expect(await screen.findByTestId('run-banner-idle')).toBeInTheDocument()
    expect(screen.queryByTestId('run-banner')).toBeNull()
    expect(window.location.search).toBe('?mission=test-board')
  })

  it('switches a board to Flow, remembers it for that board, and opens windows from rows', async () => {
    patches = installFetchMock({ boardNotes: { elements: [{ nodeId: 'fmn_frame', entries: [noteEntry('nte_1', 'human:operator', 'Keep the frame narrow')] }] } })
    const { unmount } = await renderCockpit()
    fireEvent.click(screen.getByRole('radio', { name: 'Flow' }))
    const flow = await screen.findByTestId('flow-view')
    expect(screen.getByRole('radio', { name: 'Flow' })).toBeChecked()
    expect(JSON.parse(localStorage.getItem('archon.missionView.v1') || '{}')).toEqual({ 'test-board': 'flow' })

    const mission = within(flow).getByRole('region', { name: 'Input card Showcase' })
    expect(mission).toHaveTextContent('Build the page')
    const frame = within(flow).getByTestId('flow-step-fmn_frame')
    expect(frame).toHaveTextContent('No brief yet.')
    expect(frame).toHaveTextContent('Lead (controller) is Mason (mason) on codex')
    expect(frame).toHaveTextContent('Next→ 2 Review')
    const review = within(flow).getByTestId('flow-step-gate_review')
    expect(review).toHaveTextContent('Decided by a code check')
    expect(review).toHaveTextContent('Pass→ leads nowhere: wire it to a step or an End node')
    expect(review).toHaveTextContent('Fail→ leads nowhere: wire it to a step or an End node')
    expect(within(review).getByRole('list', { name: 'Judges of Review' })).toHaveTextContent('Judge Judge')
    expect(within(flow).queryByTestId('flow-step-fmn_judge')).toBeNull()

    fireEvent.click(within(frame).getByRole('button', { name: '1 Frame' }))
    expect(await screen.findByRole('dialog', { name: 'Formation · Frame' })).toBeInTheDocument()
    fireEvent.click(within(review).getByRole('button', { name: 'Judge Judge' }))
    expect(await screen.findByRole('dialog', { name: 'Formation · Judge' })).toBeInTheDocument()
    fireEvent.click(within(frame).getByRole('button', { name: /Keep the frame narrow/ }))
    expect(await screen.findByRole('dialog', { name: 'notes for Frame' })).toBeInTheDocument()
    fireEvent.click(within(frame).getByRole('button', { name: '→ 2 Review' }))
    expect(await screen.findByRole('dialog', { name: 'Gate · Review' })).toBeInTheDocument()
    expect(patches).toEqual([])

    unmount()
    await renderCockpit()
    expect(await screen.findByTestId('flow-view')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: 'Canvas' }))
    await waitFor(() => expect(screen.queryByTestId('flow-view')).toBeNull())
    expect(localStorage.getItem('archon.missionView.v1')).toBe('{}')
  })

  it('shows each step state, attempt and block reason in Flow with a run selected', async () => {
    window.history.replaceState(null, '', '/?mission=test-board&run=run_01BLOCK')
    installRunsMock([{ runId: 'run_01BLOCK', status: 'blocked', final: false, missionSlug: 'test-board', inputCardId: 'mis_showcase', eventCount: 5 }], {
      run_01BLOCK: [
        { runId: 'run_01BLOCK', seq: 1, type: 'node_started', nodeId: 'fmn_frame', attempt: 1 },
        { runId: 'run_01BLOCK', seq: 2, type: 'node_started', nodeId: 'fmn_frame', attempt: 2 },
        { runId: 'run_01BLOCK', seq: 3, type: 'node_output', nodeId: 'fmn_frame', status: 'done' },
        { runId: 'run_01BLOCK', seq: 4, type: 'gate_evaluating', nodeId: 'gate_review', gateId: 'gate_review' },
        { runId: 'run_01BLOCK', seq: 5, type: 'run_blocked', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    const projection = globalThis.fetch
    const problems = [
      { seq: 5, type: 'run_blocked', nodeIds: ['gate_review'], reason: { text: 'invalid judge result: missing verdict block', bytes: 42 }, resumeAllowed: false },
    ]
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => String(input) === '/api/runs/run_01BLOCK/evidence/problems'
      ? Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({ success: true, data: { problems } }) } as unknown as Response)
      : projection(input, init)) as typeof fetch
    localStorage.setItem('archon.missionView.v1', JSON.stringify({ 'test-board': 'flow' }))
    await renderCockpit()

    const flow = await screen.findByTestId('flow-view')
    const frame = within(await within(flow).findByTestId('flow-step-fmn_frame')).getByLabelText('Run state of Frame')
    await waitFor(() => expect(frame).toHaveTextContent('done'))
    expect(frame).toHaveTextContent('attempt 2')
    const review = within(flow).getByLabelText('Run state of Review')
    await waitFor(() => expect(review).toHaveTextContent('blocked at Review: invalid judge result: missing verdict block'))
    fireEvent.click(within(review).getByTestId('run-point'))
    expect(await screen.findByRole('dialog', { name: 'Gate · Review' })).toBeInTheDocument()
  })

  it('offers the waiting human gate its answer panel in its Flow row', async () => {
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
    localStorage.setItem('archon.missionView.v1', JSON.stringify({ 'test-board': 'flow' }))
    installFetchMock({
      runStatus: { status: 'waiting_human', final: false },
      runEvents: [
        { runId: 'run_legacy', seq: 3, type: 'node_output', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 4, type: 'human_input_requested', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    await renderCockpit()
    const row = await screen.findByTestId('flow-step-gate_review')
    const panel = await within(row).findByRole('dialog', { name: 'Answer gate Review' })
    expect(panel).toBeInTheDocument()
    expect(within(row).getByLabelText('Run state of Review')).toHaveTextContent('waiting for you')
    expect(screen.getAllByRole('dialog', { name: 'Answer gate Review' })).toHaveLength(1)
  })

  it('answers a pending human gate from its upstream output', async () => {
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
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
      if (url === '/api/runs/run_legacy/gates/gate_review/request') {
        return reply({ request: { gateId: 'gate_review', requestedSeq: 4, criterion: 'Review the frame', input: { fromNodeId: 'fmn_frame', fromPortId: 'port_frame_out', text: questions, truncated: false } } })
      }
      if (url === '/api/runs/run_legacy/gates/gate_review/verdict') {
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

  it('keeps the gate answer in a window the run bar brings back, never under the gate editor', async () => {
    localStorage.setItem('archon.activeRun.test-board', 'run_legacy')
    installFetchMock({
      runStatus: { status: 'waiting_human', final: false },
      runEvents: [
        { runId: 'run_legacy', seq: 3, type: 'node_output', nodeId: 'fmn_frame' },
        { runId: 'run_legacy', seq: 4, type: 'human_input_requested', nodeId: 'gate_review', gateId: 'gate_review' },
      ],
    })
    const coordinator = globalThis.fetch
    ;(globalThis as Record<string, unknown>).fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/runs/run_legacy/gates/gate_review/request') {
        return Promise.resolve({ ok: true, headers: { get: () => null }, text: () => Promise.resolve(''), json: () => Promise.resolve({ success: true, data: { request: {
          gateId: 'gate_review', requestedSeq: 4, criterion: 'Review the frame', input: { fromNodeId: 'fmn_frame', text: 'frame', truncated: false },
          routes: [{ verdict: 'pass', targets: [{ nodeId: 'end_done', title: 'Done', kind: 'end', outcome: 'done' }], endsRun: true }, { verdict: 'fail', targets: [{ nodeId: 'fmn_frame', title: 'Frame', kind: 'formation', attempt: 2, rounds: { kind: 'rounds', limitId: 'lim_frame', nodeId: 'fmn_frame', used: 1, max: 2 } }] }],
        } } }) })
      }
      return coordinator(input, init)
    }) as unknown as typeof fetch
    await renderCockpit()

    const answer = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    expect(answer).toHaveAttribute('data-window-kind', 'answer')
    // A run reaching the gate does not take the keyboard from the operator.
    expect(within(answer).getByLabelText('Your response')).not.toHaveFocus()
    expect(await within(answer).findByRole('button', { name: 'Approve and end the run' })).toBeInTheDocument()
    expect(within(answer).getByText('Send back: Frame runs again with your response (round 2 of 2).')).toBeInTheDocument()

    fireEvent.click(within(answer).getByRole('button', { name: 'Close Answer gate Review' }))
    expect(screen.queryByRole('dialog', { name: 'Answer gate Review' })).toBeNull()

    // The run point brings the answer back, with the keyboard, and does not open the gate's editor over it.
    const point = screen.getByTestId('run-point')
    expect(point).toHaveAttribute('title', 'waiting for you at Review. Show it on the canvas with your answer.')
    await act(async () => { fireEvent.click(point) })
    const reopened = await screen.findByRole('dialog', { name: 'Answer gate Review' })
    await waitFor(() => expect(within(reopened).getByLabelText('Your response')).toHaveFocus())
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 600)) })
    expect(document.querySelector('[data-window-id="node:gate_review"]')).toBeNull()
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
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete Input card' }))
    await waitFor(() => {
      const removal = patches.map(patch => patch.body.deleteInputCard as { id?: string } | undefined).find(Boolean)
      expect(removal).toEqual(expect.objectContaining({ id: 'mis_showcase' }))
    })
  })

  it('uses the shared explicit Arrange operation for whole-board movement', async () => {
    await renderCockpit()
    fireEvent.click(screen.getByTestId('arrange-layout'))
    await waitFor(() => {
      expect(patches).toContainEqual({
        url: '/api/missions/test-board/layout',
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
    expect(localStorage.getItem('archon.rosterWidth')).toBe('336')
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    expect(handle).toHaveAttribute('aria-valuenow', '320')
    fireEvent.pointerDown(handle, { button: 0, clientX: 320 })
    fireEvent.pointerUp(window, { clientX: 2000 })
    expect(roster.style.getPropertyValue('--roster-width')).toBe('480px')

    fireEvent.click(screen.getByRole('button', { name: 'Collapse agent roster' }))
    expect(roster).toHaveClass('collapsed')
    expect(screen.getByRole('button', { name: 'Expand agent roster' })).toHaveAttribute('aria-expanded', 'false')
    expect(localStorage.getItem('archon.rosterCollapsed')).toBe('true')

    unmount()
    await renderCockpit()
    roster = screen.getByTestId('agent-roster')
    expect(roster).toHaveClass('collapsed')
    expect(roster.style.getPropertyValue('--roster-width')).toBe('480px')
    fireEvent.click(screen.getByRole('button', { name: 'Expand agent roster' }))
    expect(roster).not.toHaveClass('collapsed')
    expect(localStorage.getItem('archon.rosterCollapsed')).toBe('false')
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
