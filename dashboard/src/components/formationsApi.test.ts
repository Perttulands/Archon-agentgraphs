import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiRequestError,
  createBoard,
  deleteBoard,
  fetchApi,
  fetchBoardChanged,
  fetchBoardDocument,
  fetchBoardNotes,
  fetchBoardValidation,
  fetchBoardWithLayout,
  fetchRunEvents,
  normalizeBoard,
  normalizeLayout,
  patchBoardDocument,
  patchBoardNote,
  renameMission,
  startRun,
} from './formationsApi'
import type { LayoutDocument, ToolNode } from './formationsTypes'

function jsonResponse(body: unknown, options: { ok?: boolean; status?: number; etag?: string } = {}): Response {
  return {
    ok: options.ok ?? true,
    status: options.status ?? 200,
    headers: {
      get: (name: string) => name.toLowerCase() === 'etag' ? options.etag ?? null : null,
    },
    json: () => Promise.resolve(body),
  } as Response
}

// The lists the daemon sends on every mission, empty or not (archon-n7u.49).
const EMPTY_LISTS = { inputCards: [], formations: [], gates: [], tools: [], ends: [], connections: [] }

describe('formations API helpers', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('carries every run admission finding on the request error', async () => {
    const findings = [
      { code: 'unstaffed_slot', nodeId: 'fmn_plan', message: 'formation "fmn_plan" slot "Planner" (slot_plan) needs an agent' },
      { code: 'mission_not_runnable', nodeId: 'mis_main', message: 'mission "mis_main" has no outgoing connection' },
    ]
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({
      success: false,
      error: { code: 'RUN_ADMISSION_FAILED', message: 'The run needs 2 fixes before it can start', findings },
    }, { ok: false, status: 422 }))) as unknown as typeof fetch)

    const failure = await startRun('etag', { mission: 'draft', inputCardId: 'mis_main', expectedRev: 2, actor: 'agent:ui' }).catch(err => err)

    expect(failure).toBeInstanceOf(ApiRequestError)
    expect(failure).toMatchObject({ status: 422, code: 'RUN_ADMISSION_FAILED', message: 'The run needs 2 fixes before it can start', findings })
  })

  it('carries the End nodes a finished run reached into its events', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({ success: true, data: { events: [
      { seq: 2, type: 'run_failed', nodeId: 'end_rejected', gateId: 'gate_review', outcome: 'the brief misses the audience', endIds: ['end_done', 'end_rejected'] },
      { seq: 1, type: 'node_output', nodeId: 'fmn_work', status: 'done' },
    ] } }))) as unknown as typeof fetch)
    const events = await fetchRunEvents('run_1')
    expect(events.map(event => event.seq)).toEqual([1, 2])
    expect(events[1]).toMatchObject({ type: 'run_failed', nodeId: 'end_rejected', gateId: 'gate_review', data: { reason: 'the brief misses the audience', endIds: ['end_done', 'end_rejected'] } })
    expect(events[0].data).not.toHaveProperty('endIds')
  })

  it('reads board validation with empty lists when none are reported', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/missions/draft%20board/validation')
      return Promise.resolve(jsonResponse({ success: true, data: { missionRev: 3, missionEtag: 'etag-3', errors: null } }))
    }) as unknown as typeof fetch)

    await expect(fetchBoardValidation('draft board')).resolves.toEqual({ missionRev: 3, missionEtag: 'etag-3', errors: [], warnings: [] })
  })

  it('returns response data and the API ETag', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe('/api/missions')
      expect(init?.headers).toMatchObject({ 'Content-Type': 'application/json' })
      return Promise.resolve(jsonResponse({
        success: true,
        data: { missions: [{ slug: 'session-search' }] },
      }, { etag: 'board-list-etag' }))
    }) as unknown as typeof fetch)

    const result = await fetchApi<{ missions: Array<{ slug: string }> }>('/api/missions')

    expect(result).toEqual({
      data: { missions: [{ slug: 'session-search' }] },
      etag: 'board-list-etag',
    })
  })

  it('throws an ApiRequestError for explicit API failures', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({
      success: false,
      error: { code: 'NOT_FOUND', message: 'board missing' },
    }, { ok: false, status: 404 }))) as unknown as typeof fetch)

    await expect(fetchApi('/api/missions/missing')).rejects.toMatchObject({
      status: 404,
      code: 'NOT_FOUND',
      message: 'board missing',
    } satisfies Partial<ApiRequestError>)
  })

  it('fails loudly when a successful envelope omits data', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({
      success: true,
    }, { status: 200 }))) as unknown as typeof fetch)

    await expect(fetchApi('/api/missions')).rejects.toBeInstanceOf(ApiRequestError)
  })

  it('takes the response ETag and keeps the lists the daemon always sends', () => {
    const served = { id: 'brd_1', slug: 'session-search', title: 'Session search', rev: 1, etag: 'board-etag', ...EMPTY_LISTS }
    expect(normalizeBoard(served, 'response-etag')).toEqual({ ...served, etag: 'response-etag' })
    const layout: LayoutDocument = { missionId: 'brd_1', missionRev: 1, etag: 'layout-etag', nodes: [], edges: [] }
    expect(normalizeLayout(layout, 'layout-response-etag')).toEqual({ ...layout, etag: 'layout-response-etag' })
  })

  it('preserves the exact Tool projection at the API boundary', () => {
    const tool: ToolNode = {
      id: 'tool_normalize',
      title: 'Normalize report',
      profileId: 'json.normalize',
      profileVersion: '1',
      params: { mode: 'strict' },
      inputs: [{
        id: 'port_tool_in',
        name: 'input',
        label: 'Report',
        direction: 'input',
        kind: 'work',
        acceptedMediaTypes: ['application/json'],
        required: true,
        role: 'data',
      }],
      outputs: [{
        id: 'port_tool_out',
        name: 'output',
        label: 'Normalized report',
        direction: 'output',
        kind: 'work',
        acceptedMediaTypes: ['application/json'],
      }],
    }
    const board = normalizeBoard({
      id: 'brd_tool',
      slug: 'tool-board',
      title: 'Tool board',
      rev: 2,
      etag: 'tool-etag',
      formations: [],
      connections: [],
      tools: [tool],
    })

    expect(board.tools).toEqual([tool])
  })

  it('fetches and normalizes a board document', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/missions/session-search')
      return Promise.resolve(jsonResponse({
        success: true,
        data: {
          mission: {
            id: 'brd_1',
            slug: 'session-search',
            title: 'Session search',
            rev: 1,
            etag: 'board-etag',
            ...EMPTY_LISTS,
          },
        },
      }, { etag: 'response-etag' }))
    }) as unknown as typeof fetch)

    await expect(fetchBoardDocument('session-search')).resolves.toMatchObject({
      etag: 'response-etag',
      inputCards: [],
      gates: [],
      tools: [],
      formations: [],
      connections: [],
    })
  })

  it('creates a board from its human name', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return Promise.resolve(jsonResponse({
        success: true,
        data: { mission: { id: 'brd_new', slug: 'release-plan', title: 'Release Plan', rev: 1, etag: 'created-etag', ...EMPTY_LISTS } },
      }, { status: 201, etag: 'created-etag' }))
    }) as unknown as typeof fetch)

    await expect(createBoard('Release Plan')).resolves.toMatchObject({
      slug: 'release-plan',
      etag: 'created-etag',
      formations: [],
    })
    expect(calls[0].url).toBe('/api/missions')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ title: 'Release Plan' })
  })

  it('loads a fresh board with a synthetic empty layout when no sidecar exists yet', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/layout')) {
        return Promise.resolve(jsonResponse({ success: false, error: { code: 'NOT_FOUND', message: 'layout missing' } }, { ok: false, status: 404 }))
      }
      return Promise.resolve(jsonResponse({
        success: true,
        data: { mission: { id: 'brd_new', slug: 'release-plan', title: 'Release Plan', rev: 1, etag: 'created-etag', ...EMPTY_LISTS } },
      }, { etag: 'created-etag' }))
    }) as unknown as typeof fetch)

    await expect(fetchBoardWithLayout('release-plan')).resolves.toEqual({
      board: expect.objectContaining({ id: 'brd_new', formations: [] }),
      layout: { missionId: 'brd_new', missionRev: 1, etag: '*', nodes: [], edges: [] },
    })
  })

  it('deletes a board with exact optimistic concurrency inputs', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return Promise.resolve(jsonResponse({
        success: true,
        data: { deletion: { id: 'brd_1', slug: 'session-search', title: 'Session search', archiveId: 'archive_1' } },
      }))
    }) as unknown as typeof fetch)

    await expect(deleteBoard('session-search', 'board-etag', 7)).resolves.toMatchObject({ archiveId: 'archive_1' })
    expect(calls[0].url).toBe('/api/missions/session-search')
    expect(calls[0].init?.method).toBe('DELETE')
    expect(calls[0].init?.headers).toMatchObject({ 'If-Match': 'board-etag' })
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ expectedRev: 7 })
  })

  it('reads and appends to mission note threads through the shared ETag-fenced sidecar', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    const entry = { id: 'nte_1', author: 'human:ui', createdAt: '2026-08-18T13:00:00Z', text: 'shared plan' }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      calls.push({ url, init })
      const patched = init?.method === 'PATCH'
      return Promise.resolve(new Response(JSON.stringify({
        success: true,
        data: {
          notes: {
            schema: 2,
            missionId: 'brd_1',
            rev: patched ? 1 : 0,
            updatedAt: '2026-08-18T13:00:00Z',
            mission: patched ? [entry] : null,
            elements: patched ? [{ nodeId: 'fmn_a', entries: null }] : null,
            etag: patched ? 'body-etag' : '*',
          },
        },
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json', ETag: patched ? 'header-etag' : '*' },
      }))
    }))

    const empty = await fetchBoardNotes('board one')
    expect(empty.mission).toEqual([])
    expect(empty.elements).toEqual([])
    expect(empty.etag).toBe('*')

    const updated = await patchBoardNote('board one', empty.etag, { target: 'mission', action: 'append', text: 'shared plan' })
    expect(updated.mission).toEqual([entry])
    expect(updated.elements).toEqual([{ nodeId: 'fmn_a', entries: [] }])
    expect(updated.etag).toBe('header-etag')
    expect(calls[1]).toMatchObject({ url: '/api/missions/board%20one/notes' })
    expect(calls[1].init).toMatchObject({ method: 'PATCH', headers: { 'If-Match': '*' } })
    expect(JSON.parse(String(calls[1].init?.body))).toEqual({ target: 'mission', action: 'append', text: 'shared plan', author: 'human:ui' })
  })

  it('checks board changes against the current ETag', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/missions/session-search/changes?etag=board-etag')
      return Promise.resolve(jsonResponse({
        success: true,
        data: { signal: { changed: true } },
      }))
    }) as unknown as typeof fetch)

    await expect(fetchBoardChanged('session-search', 'board-etag')).resolves.toBe(true)
  })

  it('patches a board with explicit optimistic concurrency inputs', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return Promise.resolve(jsonResponse({
        success: true,
        data: {
          mission: {
            id: 'brd_1',
            slug: 'session-search',
            title: 'Session search',
            rev: 2,
            etag: 'board-etag-2',
            ...EMPTY_LISTS,
          },
          layout: {
            missionId: 'brd_1',
            missionRev: 2,
            etag: 'layout-etag-2',
            nodes: [],
            edges: [],
          },
        },
      }, { etag: 'board-response-etag' }))
    }) as unknown as typeof fetch)

    const result = await patchBoardDocument('session-search', 'board-etag', 1, { renameFormation: { id: 'fmn_1', title: 'Next' } })

    expect(calls[0].url).toBe('/api/missions/session-search')
    expect(calls[0].init?.method).toBe('PATCH')
    expect(calls[0].init?.headers).toMatchObject({ 'If-Match': 'board-etag' })
    expect(JSON.parse(String(calls[0].init?.body))).toMatchObject({
      expectedRev: 1,
      updatedBy: 'agent:ui',
      renameFormation: { id: 'fmn_1', title: 'Next' },
    })
    expect(result.board.etag).toBe('board-response-etag')
    expect(result.layout?.edges).toEqual([])
  })

  it('renames a mission as it is now, after edits elsewhere, and reads it again once if another lands in between', async () => {
    // The daemon's mission moves on: an edit elsewhere since the dialog opened, then one more mid-rename.
    let served = { id: 'brd_1', slug: 'scouting', title: 'Scouting', rev: 7, etag: 'etag-7', ...EMPTY_LISTS }
    let editsMidRename = 1
    const patches: Array<{ ifMatch: string; body: Record<string, unknown> }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method !== 'PATCH') return Promise.resolve(jsonResponse({ success: true, data: { mission: served } }, { etag: served.etag }))
      const ifMatch = (init.headers as Record<string, string>)['If-Match']
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      patches.push({ ifMatch, body })
      if (editsMidRename-- > 0) served = { ...served, rev: served.rev + 1, etag: `etag-${served.rev + 1}` }
      if (ifMatch !== served.etag || body.expectedRev !== served.rev) {
        return Promise.resolve(jsonResponse({ success: false, error: { code: 'CONFLICT', message: 'The mission changed since it was read; reload it and retry' } }, { ok: false, status: 409 }))
      }
      served = { ...served, title: String(body.title), rev: served.rev + 1, etag: `etag-${served.rev + 1}` }
      return Promise.resolve(jsonResponse({ success: true, data: { mission: served } }, { etag: served.etag }))
    }) as unknown as typeof fetch)

    const result = await renameMission('scouting', 'Field scouting')
    expect(result.board).toMatchObject({ title: 'Field scouting', rev: 9, etag: 'etag-9' })
    expect(patches.map(patch => [patch.ifMatch, patch.body.expectedRev])).toEqual([['etag-7', 7], ['etag-8', 8]])

    editsMidRename = 2
    await expect(renameMission('scouting', 'Scouting again')).rejects.toThrow('Field scouting kept changing while it was renamed; press Save to rename it as it is now')
  })

  it('starts runs with the board ETag precondition', async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = []
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(input), init })
      return Promise.resolve(jsonResponse({
        success: true,
        data: {
          runId: 'run_1',
          status: {
            runId: 'run_1',
            status: 'running',
            final: false,
            missionSlug: 'session-search',
            inputCardId: 'mis_showcase',
            eventCount: 1,
          },
        },
      }))
    }) as unknown as typeof fetch)

    await startRun('board-etag', { mission: 'session-search', inputCardId: 'mis_showcase', expectedRev: 1, actor: 'agent:ui' })

    expect(calls[0].url).toBe('/api/runs')
    expect(calls[0].init?.method).toBe('POST')
    expect(calls[0].init?.headers).toMatchObject({ 'If-Match': 'board-etag' })
    // No default limits: the run has none (archon-o7p.7).
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ mission: 'session-search', inputCardId: 'mis_showcase', expectedRev: 1, actor: 'agent:ui' })
  })
})
