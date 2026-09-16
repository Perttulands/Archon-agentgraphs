import type { RunInputs } from "./StartMissionDialog"
import { runStatusFromResponse } from './formationsRunState'
import type {
  AgentProjection,
  BoardDeletion,
  BoardDocument,
  BoardFinding,
  BoardNotesDocument,
  BoardSummary,
  BoardValidation,
  CodeGateProfileDescriptor,
  FormationNode,
  LayoutDocument,
  LayoutEdge,
  LayoutNode,
  NotePatch,
  OpenEscalation,
  PersonaCard,
  RunEvent,
  RunStartResult,
  RunStatusProjection,
  RunStatusResult,
} from './formationsTypes'

interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: { code: string; message: string; findings?: BoardFinding[] }
}

export class ApiRequestError extends Error {
  status: number
  code: string
  /** Every located problem, when the request had several (run admission). */
  findings: BoardFinding[]

  constructor(message: string, status: number, code: string, findings: BoardFinding[] = []) {
    super(message)
    this.status = status
    this.code = code
    this.findings = findings
  }
}

export async function fetchApi<T>(endpoint: string, init?: RequestInit): Promise<{ data: T; etag: string }> {
  const response = await fetch(endpoint, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers || {}),
    },
    signal: AbortSignal.timeout(10000),
  })
  const result = await response.json() as ApiResponse<T>
  if (!response.ok || !result.success || result.data === undefined || result.data === null) {
    throw new ApiRequestError(result.error?.message || `Request failed: ${response.status}`, response.status, result.error?.code || '', result.error?.findings || [])
  }
  return { data: result.data, etag: response.headers.get('ETag') || '' }
}

export function normalizeBoard(board: BoardDocument, etag = ''): BoardDocument {
  return {
    ...board,
    etag: etag || board.etag,
    missions: board.missions || [],
    formations: board.formations || [],
    gates: board.gates || [],
    tools: board.tools || [],
    connections: board.connections || [],
  }
}

export function normalizeLayout(layout: LayoutDocument, etag = ''): LayoutDocument {
  return {
    ...layout,
    etag: etag || layout.etag,
    nodes: layout.nodes || [],
    edges: layout.edges || [],
  }
}

export function missingLayoutForBoard(board: BoardDocument): LayoutDocument {
  return {
    boardId: board.id,
    boardRev: board.rev,
    etag: '*',
    nodes: [],
    edges: [],
  }
}

export async function fetchBoardSummaries(): Promise<BoardSummary[]> {
  const result = await fetchApi<{ boards: BoardSummary[] }>('/api/formations/boards')
  return result.data.boards || []
}

export async function fetchCodeGateProfiles(): Promise<CodeGateProfileDescriptor[]> {
  const result = await fetchApi<{ profiles: CodeGateProfileDescriptor[] }>('/api/formations/gate-profiles')
  return result.data.profiles || []
}

export async function fetchBoardDocument(slug: string): Promise<BoardDocument> {
  const result = await fetchApi<{ board: BoardDocument }>(`/api/formations/boards/${encodeURIComponent(slug)}`)
  return normalizeBoard(result.data.board, result.etag)
}

export async function fetchBoardValidation(slug: string): Promise<BoardValidation> {
  const result = await fetchApi<Partial<BoardValidation>>(`/api/formations/boards/${encodeURIComponent(slug)}/validation`)
  return {
    boardRev: result.data.boardRev ?? 0,
    boardEtag: result.data.boardEtag || result.etag,
    errors: result.data.errors || [],
    warnings: result.data.warnings || [],
  }
}

export async function fetchBoardLayout(slug: string): Promise<LayoutDocument> {
  const result = await fetchApi<{ layout: LayoutDocument }>(`/api/formations/boards/${encodeURIComponent(slug)}/layout`)
  return normalizeLayout(result.data.layout, result.etag)
}

export async function fetchBoardWithLayout(slug: string): Promise<{ board: BoardDocument; layout: LayoutDocument }> {
  const board = await fetchBoardDocument(slug)
  try {
    return { board, layout: await fetchBoardLayout(slug) }
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) {
      return { board, layout: missingLayoutForBoard(board) }
    }
    throw error
  }
}

export async function createBoard(title: string): Promise<BoardDocument> {
  const result = await fetchApi<{ board: BoardDocument }>('/api/formations/boards', {
    method: 'POST',
    body: JSON.stringify({ title }),
  })
  return normalizeBoard(result.data.board, result.etag)
}

export async function deleteBoard(slug: string, etag: string, rev: number): Promise<BoardDeletion> {
  const result = await fetchApi<{ deletion: BoardDeletion }>(
    `/api/formations/boards/${encodeURIComponent(slug)}`,
    {
      method: 'DELETE',
      headers: { 'If-Match': etag },
      body: JSON.stringify({ expectedRev: rev }),
    },
  )
  return result.data.deletion
}

function normalizeNotes(notes: BoardNotesDocument, etag: string): BoardNotesDocument {
  return {
    ...notes,
    board: notes.board || [],
    elements: (notes.elements || []).map(element => ({ ...element, entries: element.entries || [] })),
    etag: etag || notes.etag,
  }
}

export async function fetchBoardNotes(slug: string): Promise<BoardNotesDocument> {
  const result = await fetchApi<{ notes: BoardNotesDocument }>(`/api/formations/boards/${encodeURIComponent(slug)}/notes`)
  return normalizeNotes(result.data.notes, result.etag)
}

/** The cockpit writes notes as the operator, human:ui. */
export async function patchBoardNote(slug: string, etag: string, patch: NotePatch): Promise<BoardNotesDocument> {
  const result = await fetchApi<{ notes: BoardNotesDocument }>(`/api/formations/boards/${encodeURIComponent(slug)}/notes`, {
    method: 'PATCH',
    headers: { 'If-Match': etag },
    body: JSON.stringify({ ...patch, author: 'human:ui' }),
  })
  return normalizeNotes(result.data.notes, result.etag)
}

export async function fetchAgents(): Promise<AgentProjection[]> {
  const result = await fetchApi<{ agents: AgentProjection[] }>('/api/agents')
  return result.data.agents || []
}

export async function fetchAgentCard(agentID: string): Promise<PersonaCard> {
  const result = await fetchApi<PersonaCard>(`/api/agents/${encodeURIComponent(agentID)}`)
  return { ...result.data, etag: result.etag || result.data.etag }
}

export async function overrideAgentCard(agentID: string, etag: string, patch: {
  displayName: string
  kind: string
  summary: string
  capabilities: string[]
  sessionStem: string
  launch: string
}): Promise<PersonaCard> {
  const result = await fetchApi<PersonaCard>(`/api/agents/${encodeURIComponent(agentID)}`, {
    method: 'PATCH',
    headers: { 'If-Match': etag },
    body: JSON.stringify(patch),
  })
  return { ...result.data, etag: result.etag || result.data.etag }
}

export async function fetchBoardChanged(slug: string, etag: string): Promise<boolean> {
  const result = await fetchApi<{ signal: { changed?: boolean } }>(
    `/api/formations/boards/${encodeURIComponent(slug)}/changes?etag=${encodeURIComponent(etag)}`
  )
  return result.data.signal?.changed === true
}

type PatchBoardResponse<TExtra extends object> = {
  board: BoardDocument
  layout?: LayoutDocument
} & TExtra

export async function patchBoardDocument<TExtra extends object = Record<string, never>>(
  slug: string,
  etag: string,
  rev: number,
  patch: Record<string, unknown>,
): Promise<PatchBoardResponse<TExtra>> {
  const result = await fetchApi<PatchBoardResponse<TExtra>>(
    `/api/formations/boards/${encodeURIComponent(slug)}`,
    {
      method: 'PATCH',
      headers: { 'If-Match': etag },
      body: JSON.stringify({
        expectedRev: rev,
        updatedBy: 'agent:ui',
        ...patch,
      }),
    }
  )
  return {
    ...result.data,
    board: normalizeBoard(result.data.board, result.etag),
    layout: result.data.layout ? normalizeLayout(result.data.layout) : undefined,
  }
}

export async function patchBoardLayout(slug: string, etag: string, patch: { nodes?: LayoutNode[]; edges?: LayoutEdge[]; arrange?: boolean }): Promise<LayoutDocument> {
  const result = await fetchApi<{ layout: LayoutDocument }>(
    `/api/formations/boards/${encodeURIComponent(slug)}/layout`,
    {
      method: 'PATCH',
      headers: { 'If-Match': etag },
      body: JSON.stringify(patch),
    }
  )
  return normalizeLayout(result.data.layout, result.etag)
}

export async function startRun(etag: string, body: { board: string; missionId?: string; formationId?: string; expectedRev: number; actor: string } & Partial<RunInputs>): Promise<RunStartResult> {
  const result = await fetchApi<{ runId: string }>('/api/formations/runs', {
    method: 'POST',
    headers: { 'If-Match': etag },
    body: JSON.stringify({ ...body, limits: body.limits ?? { maxDispatch: 20, maxAttempts: 3, wallClockSeconds: 1800, redact: false } }),
  })
  return { runId: result.data.runId, status: runStatusFromResponse(await fetchRunStatus(result.data.runId)) }
}

/** Lists a board's runs from the daemon; a response without a run list yields none. */
export async function fetchBoardRuns(slug: string): Promise<RunStatusProjection[]> {
  const result = await fetchApi<RunStatusProjection[]>(`/api/formations/runs?board=${encodeURIComponent(slug)}`)
  return Array.isArray(result.data) ? result.data.filter(run => !run.boardSlug || run.boardSlug === slug) : []
}

export async function fetchRunStatus(runId: string): Promise<RunStatusProjection | RunStatusResult> {
  const result = await fetchApi<RunStatusProjection | RunStatusResult>(`/api/formations/runs/${encodeURIComponent(runId)}`)
  return result.data
}

export async function fetchRunEvents(runId: string): Promise<RunEvent[]> {
  const result = await fetchApi<{ events: Array<{ seq: number; type: string; nodeId?: string; slotId?: string; gateId?: string; attempt?: number; status?: string; verdict?: string; sessionName?: string; outcome?: string }> }>(`/api/formations/runs/${encodeURIComponent(runId)}/events`)
  return (result.data.events || []).map(event => ({
    seq: event.seq, type: event.type, runId, nodeId: event.nodeId, gateId: event.gateId, attempt: event.attempt,
    data: { slotId: event.slotId, status: event.status, verdict: event.verdict, sessionRef: event.sessionName, reason: event.outcome },
  })).sort((a, b) => a.seq - b.seq)
}

export async function fetchRunEscalations(runId: string): Promise<OpenEscalation[]> {
  const result = await fetchApi<{ escalations: OpenEscalation[] }>(`/api/formations/runs/${encodeURIComponent(runId)}/escalations`)
  return (result.data.escalations || []).sort((a, b) => a.seq - b.seq)
}

export async function abortRunRequest(runId: string, body: { reason: string; requestedBy: string }): Promise<RunStatusProjection | RunStatusResult> {
  const result = await fetchApi<RunStatusProjection | RunStatusResult>(
    `/api/formations/runs/${encodeURIComponent(runId)}/abort`,
    {
      method: 'POST',
      body: JSON.stringify(body),
    }
  )
  return result.data
}

export async function resumeRunRequest(runId: string, body: { actor: string; mode: string; reason: string }): Promise<RunStatusProjection | RunStatusResult> {
  const result = await fetchApi<RunStatusProjection | RunStatusResult>(
    `/api/formations/runs/${encodeURIComponent(runId)}/resume`,
    {
      method: 'POST',
      body: JSON.stringify(body),
    }
  )
  return result.data
}

export interface HumanGateRequest {
  gateId: string
  requestedSeq: number
  criterion: string
  input: { fromNodeId?: string; fromPortId?: string; text: string; truncated: boolean }
}

export async function fetchHumanGateRequest(runId: string, gateId: string): Promise<HumanGateRequest> {
  const result = await fetchApi<{ request: HumanGateRequest }>(
    `/api/formations/runs/${encodeURIComponent(runId)}/gates/${encodeURIComponent(gateId)}/request`,
  )
  return result.data.request
}

export async function recordGateVerdict(runId: string, gateId: string, body: { actor: string; verdict: 'pass' | 'fail'; reason: string; requestedSeq: number }): Promise<RunStatusProjection | RunStatusResult> {
  await fetchApi<{ runId: string }>(
    `/api/formations/runs/${encodeURIComponent(runId)}/gates/${encodeURIComponent(gateId)}/verdict`,
    {
      method: 'POST',
      body: JSON.stringify(body),
    }
  )
  return fetchRunStatus(runId)
}

export type PatchCreateFormationResult = {
  layout: LayoutDocument
  formation: FormationNode
}
