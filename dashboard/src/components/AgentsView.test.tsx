import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsView, { orderReachableItems, reachableMissionItems } from './AgentsView'
import type { BoardDocument, LayoutDocument } from './formationsTypes'

describe('AgentsView', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    window.localStorage.clear()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('computes mission reachability from connections across branched gate paths', () => {
    const board = missionBoard()

    expect(reachableMissionItems(board, 'mission-alpha').map(item => {
      const via = item.via ? `${item.via.gateId}/${item.via.branch}` : 'main'
      return `${item.kind}:${item.id}:${via}`
    })).toEqual([
      'formation:authoring:main',
      'gate:human-review:main',
      'formation:fix-pass:human-review/pass',
      'formation:escalate-fail:human-review/fail',
    ])
  })

  it('orders staffing by the wiring from the mission, using the canvas only between parallel branches', () => {
    const board = missionBoard()
    const layout: LayoutDocument = {
      boardId: 'board-1', boardRev: 7, etag: 'layout-etag',
      nodes: [
        { id: 'escalate-fail', x: 900, y: 100 },
        { id: 'human-review', x: 600, y: 100 },
        { id: 'fix-pass', x: 900, y: 400 },
        { id: 'authoring', x: 100, y: 700 },
      ],
    }
    expect(orderReachableItems(reachableMissionItems(board, 'mission-alpha'), layout).map(item => item.id))
      .toEqual(['authoring', 'human-review', 'escalate-fail', 'fix-pass'])
    expect(orderReachableItems(reachableMissionItems(board, 'mission-alpha'), null).map(item => item.id))
      .toEqual(['authoring', 'human-review', 'fix-pass', 'escalate-fail'])
  })

  it('loads mission staffing and preserves the board patch contract for assign and unassign', async () => {
    const board = missionBoard()
    const layout = missionLayout()
    const patches: Array<{ headers: HeadersInit | undefined; body: unknown }> = []
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: {
            agents: [
              agent('coder', { displayName: 'Coder', liveness: 'live', harnessDefault: 'openai-codex' }),
              agent('susie', { displayName: 'Susie', tags: ['design'], liveness: 'offline' }),
            ],
            count: 2,
          },
        }))
      }
      if (url === '/api/agents/coder') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('coder', { displayName: 'Coder', harnessDefault: 'openai-codex', harnessVariants: [{ id: 'openai-codex', sessionStem: 'coder' }] }) }, 200, { ETag: 'coder-etag' }))
      }
      if (url === '/api/agents/susie') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName: 'Susie', harnessVariants: [{ id: 'claude-code', sessionStem: 'susie', source: '/tmp/SUSIE.toml' }] }) }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/boards/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/mission-board' && init?.method === 'PATCH') {
        patches.push({ headers: init.headers, body: JSON.parse(String(init.body)) })
        return Promise.resolve(jsonResponse({ success: true, data: { board: { ...board, etag: 'board-etag-2' } } }, 200, { ETag: 'board-etag-2' }))
      }
      if (url === '/api/formations/boards/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Authoring')).toBeInTheDocument()
    expect(screen.getByText('Fix Pass')).toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'Agent roster' })).getByText('2 · 1 live · 2 on mission')).toBeInTheDocument()
    expect(screen.getByText('Escalate Fail')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /assign Review slot/i }))
    expect(await screen.findByText('missing harness variant openai-codex')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /assign coder/i }))

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('board-etag')
    expect(patches[0].body).toMatchObject({
      expectedRev: 7,
      updatedBy: 'agent:ui',
      assignSlot: { formationId: 'authoring', slotId: 'reviewer', agentId: 'coder', harness: 'openai-codex' },
    })

    fireEvent.click(screen.getByRole('button', { name: /inspect Lead slot assigned to Susie/i }))
    fireEvent.click(await screen.findByRole('button', { name: /unassign Susie/i }))

    await waitFor(() => expect(patches).toHaveLength(2))
    expect(patches[1].body).toMatchObject({
      expectedRev: 7,
      updatedBy: 'agent:ui',
      assignSlot: { formationId: 'authoring', slotId: 'lead', agentId: '', harness: '' },
    })
  })

  it('names a gate checker in operator words instead of the internal kind', async () => {
    const board = { ...missionBoard(), gates: [{ id: 'human-review', title: 'Human Review', kinds: ['formation', 'human'], criterion: 'Approve the branch.' }] }
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/boards/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    const kinds = await screen.findByTestId('gate-kinds-human-review')
    expect(within(kinds).getAllByText(/judge|human/).map(chip => chip.textContent)).toEqual(['judge', 'human'])
    const gateCard = kinds.closest('.gatecard') as HTMLElement
    expect(gateCard.querySelector('.gs')).toHaveTextContent(/^Approve the branch\.$/)
    expect(gateCard.textContent).not.toMatch(/formation/)
  })

  it('reports the mission run from the daemon read-only and links it to Boards', async () => {
    const board = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/boards/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/runs?board=mission-board') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: [
            { ...runStatus('run_01A_old', 'mission-alpha'), status: 'succeeded', final: true },
            { ...runStatus('run_01B_cli', 'mission-alpha'), status: 'waiting_human', waitingGates: [{ gateId: 'human-review', requestedSeq: 4 }] },
            { ...runStatus('run_01C_other', 'mission-beta'), status: 'running' },
          ],
        }))
      }
      if (url === '/api/formations/runs/run_01B_cli/events') {
        return Promise.resolve(jsonResponse({ success: true, data: { events: [{ seq: 3, type: 'node_output', nodeId: 'authoring', status: 'done' }, { seq: 4, type: 'human_input_requested', nodeId: 'human-review', gateId: 'human-review' }] } }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    const run = await screen.findByTestId('mission-run')
    await waitFor(() => expect(run).toHaveTextContent('waiting_human'))
    expect(run).toHaveTextContent('waiting on Human Review')
    expect(within(run).getByRole('link', { name: 'Open on Boards' })).toHaveAttribute('href', '?board=mission-board&run=run_01B_cli')
    for (const action of [/start mission/i, /^pass$/i, /^fail$/i, /^resume/i, /^stop$/i]) {
      expect(screen.queryByRole('button', { name: action })).toBeNull()
    }
    expect(fetchMock.mock.calls.some(([, init]) => init && (init as RequestInit).method && (init as RequestInit).method !== 'GET')).toBe(false)
    expect(window.localStorage.length).toBe(0)
  })

  it('names why the mission run is blocked from its run evidence', async () => {
    const board = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/boards/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/runs?board=mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: [{ ...runStatus('run_01D_blocked', 'mission-alpha'), status: 'blocked', resumeAllowed: false }] }))
      }
      if (url === '/api/formations/runs/run_01D_blocked/events') {
        return Promise.resolve(jsonResponse({ success: true, data: { events: [{ seq: 7, type: 'run_blocked', nodeId: 'human-review', gateId: 'human-review' }] } }))
      }
      if (url === '/api/formations/runs/run_01D_blocked/evidence/problems') {
        return Promise.resolve(jsonResponse({ success: true, data: { problems: [{ seq: 7, type: 'run_blocked', nodeIds: ['human-review'], reason: { text: 'invalid judge result: expected exactly one chrote-verdict block', bytes: 64 }, resumeAllowed: false }] } }))
      }
      if (url === '/api/formations/runs?board=empty') return Promise.resolve(jsonResponse({ success: true, data: [] }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    const run = await screen.findByTestId('mission-run')
    await waitFor(() => expect(run).toHaveTextContent('blocked: invalid judge result: expected exactly one chrote-verdict block'))
    expect(within(run).getByRole('link', { name: 'Open on Boards' })).toHaveAttribute('href', '?board=mission-board&run=run_01D_blocked')
  })

  it('edits a persona from the Agents tab with the shared persona editor', async () => {
    const board = emptyBoard()
    const patches: Array<{ headers: HeadersInit | undefined; body: unknown }> = []
    let displayName = 'Susie'
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('susie', { displayName, harnessDefault: 'claude-code' })], count: 1 } }))
      if (url === '/api/agents/susie' && init?.method === 'PATCH') {
        const body = JSON.parse(String(init.body))
        patches.push({ headers: init.headers, body })
        displayName = body.displayName
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName }) }, 200, { ETag: 'susie-etag-2' }))
      }
      if (url === '/api/agents/susie') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName, summary: 'Designs things', harnessVariants: [{ id: 'claude-code', sessionStem: 'susie', launch: 'claude' }] }) }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/boards/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /inspect Susie/i }))
    fireEvent.click(await screen.findByRole('button', { name: 'Edit persona' }))
    const editor = await screen.findByTestId('persona-editor')
    const name = await within(editor).findByLabelText('Agent display name')
    expect(within(editor).getByLabelText('Agent summary')).toHaveValue('Designs things')
    fireEvent.change(name, { target: { value: 'Susie Designer' } })
    fireEvent.click(within(editor).getByRole('button', { name: 'Save agent override' }))

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('susie-etag')
    expect(patches[0].body).toMatchObject({ displayName: 'Susie Designer', summary: 'Designs things', launch: 'claude' })
    await waitFor(() => expect(screen.queryByTestId('persona-editor')).toBeNull())
    expect(await screen.findByRole('button', { name: /inspect Susie Designer/i })).toBeInTheDocument()
  })

  it('offers a board retry when the selected board fails to load', async () => {
    const board = emptyBoard()
    let failBoard = true
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/boards/empty/layout') {
        if (failBoard) return Promise.reject(new Error('layout unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/empty') {
        if (failBoard) return Promise.reject(new Error('board unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText(/Board load failed:/)).toBeInTheDocument()
    failBoard = false
    fireEvent.click(screen.getByRole('button', { name: /retry board/i }))

    expect(await screen.findByText(/No personas yet/)).toBeInTheDocument()
    expect(screen.queryByText(/Board load failed:/)).not.toBeInTheDocument()
  })

  it('keeps slot eligibility usable when one persona detail load fails', async () => {
    const board = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: {
            agents: [
              agent('good', { displayName: 'Good', liveness: 'live', harnessDefault: 'openai-codex' }),
              agent('broken', { displayName: 'Broken', liveness: 'live', harnessDefault: 'openai-codex' }),
            ],
            count: 2,
          },
        }))
      }
      if (url === '/api/agents/good') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('good', { displayName: 'Good', harnessDefault: 'openai-codex', harnessVariants: [{ id: 'openai-codex', sessionStem: 'good' }] }) }, 200, { ETag: 'good-etag' }))
      }
      if (url === '/api/agents/broken') {
        return Promise.reject(new Error('detail failed'))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/boards/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/mission-board' && init?.method === 'PATCH') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/boards/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /assign Review slot/i }))

    expect(await screen.findByRole('button', { name: /assign Good/i })).toBeEnabled()
    expect(screen.getByText('failed detail load')).toBeInTheDocument()
    expect(screen.queryByText('Agent eligibility request failed')).not.toBeInTheDocument()
  })

  it('groups personas by harness with harness marks and states only what differs from offline', async () => {
    const board = emptyBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: {
            agents: [
              agent('attached-live', { displayName: 'Attached Live', liveness: 'live', attached: true, harnessDefault: 'openai-codex' }),
              agent('ambiguous-one', { displayName: 'Ambiguous One', liveness: 'ambiguous', harnessDefault: 'claude-code' }),
              agent('offline-one', { displayName: 'Offline One', liveness: 'offline' }),
              agent('retired-one', { displayName: 'Retired One', liveness: 'offline', assignable: false }),
              agent('floating-session', { displayName: 'floating-session', liveness: 'live', unbound: true, assignable: false }),
            ],
            count: 5,
          },
        }))
      }
      if (url === '/api/agents/retired-one') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('retired-one', { displayName: 'Retired One', status: 'retired' }) }, 200, { ETag: 'retired-etag' }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/boards/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Attached Live')).toBeInTheDocument()
    const roster = screen.getByRole('complementary', { name: 'Agent roster' })
    expect(Array.from(roster.querySelectorAll('.roster-group-label')).map(label => label.textContent)).toEqual(['Codex', 'Claude', 'Other', 'Unbound'])

    const codexRow = within(roster).getByRole('button', { name: /inspect Attached Live/i })
    expect(codexRow.querySelector('.av svg')).not.toBeNull()
    expect(within(codexRow).queryByText('AT')).not.toBeInTheDocument()
    expect(within(roster).getByRole('button', { name: /inspect Ambiguous One/i }).querySelector('.av svg')).not.toBeNull()

    expect(within(roster).getAllByText('live')).toHaveLength(2)
    expect(within(roster).getByText('ambiguous')).toBeInTheDocument()
    expect(within(roster).queryByText('offline')).not.toBeInTheDocument()
    expect(within(roster).getByText('attached')).toBeInTheDocument()
    expect(within(roster).getByText('not assignable')).toBeInTheDocument()
    expect(within(roster).getByText('no persona')).toBeInTheDocument()
    expect(screen.queryByRole('complementary', { name: 'Inspector' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /inspect Retired One/i }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    await waitFor(() => expect(within(inspector).getByText('retired')).toBeInTheDocument())
    expect(within(inspector).getByText('offline')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /inspect floating-session/i }))
    fireEvent.click(screen.getByRole('button', { name: /create persona from this session/i }))
    expect(screen.getByLabelText('Agent id')).toHaveValue('floating-session')
    expect(screen.getByLabelText('Session stem')).toHaveValue('floating-session')
  })

  it('creates a persona with canonical harness metadata instead of a legacy codex alias', async () => {
    const postedBodies: unknown[] = []
    const board = emptyBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents' && init?.method === 'POST') {
        postedBodies.push(JSON.parse(String(init.body)))
        return Promise.resolve(jsonResponse({ success: true, data: persona('writer', { displayName: 'Writer', harnessDefault: 'openai-codex', harnessVariants: [{ id: 'openai-codex', sessionStem: 'writer', launch: 'codex --profile writer' }] }) }, 201, { ETag: 'writer-etag' }))
      }
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/boards/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /new agent/i }))
    expect(screen.queryByRole('option', { name: 'codex' })).toBeNull()
    fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'writer' } })
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Writer' } })
    fireEvent.change(screen.getByLabelText('Harness'), { target: { value: 'openai-codex' } })
    fireEvent.change(screen.getByLabelText('Summary'), { target: { value: 'Writes launch copy' } })
    fireEvent.change(screen.getByLabelText('Launch'), { target: { value: 'codex --profile writer' } })
    fireEvent.change(screen.getByLabelText('Capabilities'), { target: { value: 'writing, voice' } })
    fireEvent.click(screen.getByRole('button', { name: /^Create persona$/i }))

    await waitFor(() => expect(postedBodies).toHaveLength(1))
    expect(postedBodies[0]).toMatchObject({
      id: 'writer',
      displayName: 'Writer',
      kind: 'specialist',
      harness: 'openai-codex',
      summary: 'Writes launch copy',
      launch: 'codex --profile writer',
      capabilities: ['writing', 'voice'],
    })
  })

  it('uses persona detail ETags for edits and keeps user input visible on 409 and 428 conflicts', async () => {
    const board = emptyBoard()
    const patchStatuses = [409, 428]
    const patchHeaders: Array<HeadersInit | undefined> = []
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('susie', { displayName: 'Susie', tags: ['design'] })], count: 1 } }))
      }
      if (url === '/api/agents/susie' && init?.method === 'PATCH') {
        patchHeaders.push(init.headers)
        const status = patchStatuses.shift() || 409
        const message = status === 428 ? 'If-Match precondition is required' : 'Agent card changed; reload and retry'
        const code = status === 428 ? 'PRECONDITION_REQUIRED' : 'CONFLICT'
        return Promise.resolve(jsonResponse({ success: false, error: { code, message } }, status))
      }
      if (url === '/api/agents/susie') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: persona('susie', {
            displayName: 'Susie',
            tags: ['design'],
            harnessVariants: [{ id: 'claude-code', sessionStem: 'susie', source: '/tmp/SUSIE.toml' }],
            toml: 'CLAUDE.md contents',
          }),
        }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/formations/boards') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/boards/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/boards/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /inspect Susie/i }))
    expect(await screen.findByText('/tmp/SUSIE.toml')).toBeInTheDocument()
    expect(screen.queryByText('CLAUDE.md contents')).not.toBeInTheDocument()

    const note = screen.getByLabelText('Add note')
    fireEvent.change(note, { target: { value: 'Keep this edit' } })
    fireEvent.click(screen.getByRole('button', { name: /save note/i }))

    expect(await screen.findByText('Agent card changed; reload and retry')).toBeInTheDocument()
    expect(headerValue(patchHeaders[0], 'If-Match')).toBe('susie-etag')
    expect(screen.getByDisplayValue('Keep this edit')).toBeInTheDocument()

    fireEvent.change(note, { target: { value: 'Still visible' } })
    fireEvent.click(screen.getByRole('button', { name: /save note/i }))

    expect(await screen.findByText('Programming error: If-Match precondition is required')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Still visible')).toBeInTheDocument()
  })
})

function missionBoard(): BoardDocument {
  return {
    id: 'board-1',
    slug: 'mission-board',
    title: 'Mission Board',
    rev: 7,
    etag: 'board-etag',
    missions: [{ id: 'mission-alpha', title: 'Mission Alpha', goal: 'Ship the redesign', beadId: 'chrt-hgc9' }],
    formations: [
      {
        id: 'authoring',
        type: 'peer',
        title: 'Authoring',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [
          { id: 'lead', label: 'Lead', controller: true, agentId: 'susie', harness: 'claude-code' },
          { id: 'reviewer', label: 'Review', controller: false, harness: 'openai-codex' },
        ],
      },
      {
        id: 'fix-pass',
        type: 'solo',
        title: 'Fix Pass',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [{ id: 'builder', label: 'Builder', controller: true, harness: 'openai-codex' }],
      },
      {
        id: 'escalate-fail',
        type: 'solo',
        title: 'Escalate Fail',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [{ id: 'critic', label: 'Critic', controller: true, agentId: 'coder', harness: 'openai-codex' }],
      },
    ],
    gates: [{ id: 'human-review', title: 'Human Review', kinds: ['human'], criterion: 'Approve the branch.' }],
    connections: [
      { id: 'c1', from: 'mission-alpha:out', to: 'authoring:in' },
      { id: 'c2', from: 'authoring:out', to: 'human-review:in' },
      { id: 'c3', from: 'human-review:pass', to: 'fix-pass:in' },
      { id: 'c4', from: 'human-review:fail', to: 'escalate-fail:in' },
    ],
  }
}

function missionLayout(): LayoutDocument {
  return {
    boardId: 'board-1',
    boardRev: 7,
    etag: 'layout-etag',
    nodes: [
      { id: 'authoring', x: 100, y: 100 },
      { id: 'human-review', x: 250, y: 100 },
      { id: 'fix-pass', x: 400, y: 60 },
      { id: 'escalate-fail', x: 400, y: 180 },
    ],
  }
}

function emptyBoard(): BoardDocument {
  return {
    id: 'empty',
    slug: 'empty',
    title: 'Empty',
    rev: 1,
    etag: 'empty-etag',
    missions: [],
    formations: [],
    gates: [],
    connections: [],
  }
}

function emptyLayout(): LayoutDocument {
  return { boardId: 'empty', boardRev: 1, etag: 'layout-etag', nodes: [] }
}

function runStatus(runId: string, missionId: string) {
  return {
    runId,
    status: 'running',
    final: false,
    boardSlug: 'mission-board',
    missionId,
    eventCount: 0,
  }
}

function agent(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    displayName: id,
    kind: 'specialist',
    tags: [],
    liveness: 'offline',
    assignable: true,
    ...overrides,
  }
}

function persona(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    displayName: id,
    kind: 'specialist',
    summary: '',
    tags: [],
    status: '',
    harnessDefault: 'claude-code',
    harnessVariants: [{ id: 'claude-code', sessionStem: id, source: '/tmp/AGENT.toml' }],
    notes: [],
    etag: `${id}-etag`,
    ...overrides,
  }
}

function jsonResponse(body: unknown, status = 200, headers: Record<string, string> = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (name: string) => headers[name] ?? headers[name.toLowerCase()] ?? '',
    },
    json: () => Promise.resolve(body),
  } as Response
}

function headerValue(headers: HeadersInit | undefined, key: string): string {
  if (!headers) return ''
  if (headers instanceof Headers) return headers.get(key) || ''
  if (Array.isArray(headers)) {
    const pair = headers.find(([name]) => name.toLowerCase() === key.toLowerCase())
    return pair?.[1] || ''
  }
  return headers[key] || headers[key.toLowerCase()] || ''
}
