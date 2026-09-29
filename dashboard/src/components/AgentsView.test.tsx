import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsView, { orderReachableItems, reachableMissionItems } from './AgentsView'
import type { BoardDocument, LayoutDocument } from './formationsTypes'

describe('AgentsView', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    window.localStorage.clear()
    window.history.replaceState(null, '', '/')
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

  it('reaches every judge in a reached gate\'s judge chain, and the work behind a Tool', () => {
    const items = reachableMissionItems(judgedBoard(), 'mission')
    expect(items.map(item => {
      const via = item.via ? `${item.via.gateId}/${item.via.branch}` : 'main'
      return `${item.kind}:${item.id}:${via}:${item.depth}`
    })).toEqual([
      'formation:build:main:1',
      'gate:review:main:3',
      'formation:judge-a:review/judge:4',
      'formation:judge-b:review/judge:4',
      'formation:ship:review/pass:4',
    ])
    expect(orderReachableItems(items, null).map(item => item.id)).toEqual(['build', 'review', 'judge-a', 'judge-b', 'ship'])
  })

  it('lists each reachable node once through fail loops, gates reached twice and fail edges into a judge', () => {
    const label = (items: ReturnType<typeof reachableMissionItems>) => items.map(item => `${item.kind}:${item.id}:${item.via ? `${item.via.gateId}/${item.via.branch}` : 'main'}`)
    const base = judgedBoard()

    // review:fail -> build:in loops back to work already reached; the walk ends and adds nothing.
    const loop = { ...base, connections: [...base.connections, { id: 'loop', from: 'review:fail', to: 'build:in' }] }
    expect(label(reachableMissionItems(loop, 'mission'))).toEqual([
      'formation:build:main', 'gate:review:main', 'formation:judge-a:review/judge', 'formation:judge-b:review/judge', 'formation:ship:review/pass',
    ])

    // Two paths into one gate: the gate and its judges appear once.
    const twoPaths = {
      ...base,
      formations: [...base.formations, { ...base.formations[0], id: 'other', title: 'Other' }],
      connections: [...base.connections, { id: 'm2', from: 'mission:out', to: 'other:in' }, { id: 'o1', from: 'other:out', to: 'review:in' }],
    }
    const twice = reachableMissionItems(twoPaths, 'mission')
    for (const id of ['review', 'judge-a', 'judge-b', 'ship']) expect(twice.filter(item => item.id === id)).toHaveLength(1)
    expect(twice.find(item => item.id === 'judge-a')?.via).toEqual({ gateId: 'review', branch: 'judge' })

    // A second gate's fail edge into a judge: the judge is still one card and one slot.
    const failToJudge = {
      ...base,
      gates: [...(base.gates || []), { id: 'recheck', title: 'Recheck', kinds: ['human'], criterion: 'Looks right.' }],
      connections: [...base.connections.filter(edge => edge.id !== 'c7'), { id: 'p1', from: 'review:pass', to: 'recheck:in' }, { id: 'f1', from: 'recheck:fail', to: 'judge-a:in' }],
    } as BoardDocument
    const judged = reachableMissionItems(failToJudge, 'mission')
    expect(judged.filter(item => item.id === 'judge-a')).toHaveLength(1)
    expect(label(judged)).toEqual([
      // Reached both as judges and on a fail route, the chain carries no single provenance.
      'formation:build:main', 'gate:review:main', 'formation:judge-a:main', 'formation:judge-b:main', 'gate:recheck:review/pass',
    ])
  })

  it('counts judge slots in readiness and in a judge persona\'s slots on this mission', async () => {
    const board = judgedBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('critic', { displayName: 'Critic' }), agent('builder', { displayName: 'Builder' })], count: 2 } }))
      }
      if (url === '/api/agents/critic') return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { displayName: 'Critic' }) }, 200, { ETag: 'critic-etag' }))
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag' }] } }))
      }
      if (url === '/api/formations/missions/judged/layout') return Promise.resolve(jsonResponse({ success: true, data: { layout: { boardId: 'judged', boardRev: 3, etag: 'l', nodes: [] } } }, 200, { ETag: 'l' }))
      if (url === '/api/formations/missions/judged') return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'judged-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Second judge')).toBeInTheDocument()
    expect(screen.getByText('First judge')).toBeInTheDocument()
    expect(screen.getAllByText('judges Review')).toHaveLength(2)
    expect(screen.getByText('judged by First judge → Second judge')).toBeInTheDocument()
    expect(screen.getByText('3/4 slots staffed · 1 open')).toBeInTheDocument()
    expect(screen.queryByText(/ready/)).not.toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'Agent roster' })).getByText('2 · 2 on mission')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    expect(within(inspector).getByText('First judge')).toBeInTheDocument()
    expect(within(inspector).queryByText('No slots on this mission.')).not.toBeInTheDocument()
  })

  it('opens the shared current board and makes a board chosen here current for Boards too', async () => {
    const judged = judgedBoard()
    const mission = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [
          { id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' },
          { id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag' },
        ] } }))
      }
      if (url.endsWith('/layout')) return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'l' }))
      if (url === '/api/formations/missions/judged') return Promise.resolve(jsonResponse({ success: true, data: { board: judged } }, 200, { ETag: 'judged-etag' }))
      if (url === '/api/formations/missions/mission-board') return Promise.resolve(jsonResponse({ success: true, data: { board: mission } }, 200, { ETag: 'board-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })
    window.localStorage.setItem('archon.currentBoard.v1', 'judged')

    render(<AgentsView />)

    expect(await screen.findByText('First judge')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Mission' })).toHaveValue('judged')
    expect(screen.getByRole('combobox', { name: 'Input card' })).toBeInTheDocument()
    // Apart from the missions' own titles in the picker, nothing says board.
    const view = screen.getByTestId('agents-view').cloneNode(true) as HTMLElement
    view.querySelectorAll('option').forEach(option => option.remove())
    const words = [view.textContent || '', ...[...view.querySelectorAll('[aria-label],[title]')].map(element => `${element.getAttribute('aria-label') || ''} ${element.getAttribute('title') || ''}`)].join('\n')
    expect(words).not.toMatch(/\bboards?\b/i)
    expect(window.location.search).toBe('?mission=judged')

    fireEvent.change(screen.getByRole('combobox', { name: 'Mission' }), { target: { value: 'mission-board' } })
    expect(await screen.findByText('Authoring')).toBeInTheDocument()
    expect(window.location.search).toBe('?mission=mission-board')
    expect(window.localStorage.getItem('archon.currentBoard.v1')).toBe('mission-board')
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/mission-board' && init?.method === 'PATCH') {
        patches.push({ headers: init.headers, body: JSON.parse(String(init.body)) })
        return Promise.resolve(jsonResponse({ success: true, data: { board: { ...board, etag: 'board-etag-2' } } }, 200, { ETag: 'board-etag-2' }))
      }
      if (url === '/api/formations/missions/mission-board') {
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/mission-board') {
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/runs?mission=mission-board') {
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
    await waitFor(() => expect(run).toHaveTextContent('Waiting for your answer'))
    expect(run).toHaveTextContent('waiting on Human Review')
    expect(within(run).getByRole('link', { name: 'Open on Missions' })).toHaveAttribute('href', '?mission=mission-board&run=run_01B_cli')
    for (const action of [/start mission/i, /^pass$/i, /^fail$/i, /^resume/i, /^stop$/i]) {
      expect(screen.queryByRole('button', { name: action })).toBeNull()
    }
    expect(fetchMock.mock.calls.some(([, init]) => init && (init as RequestInit).method && (init as RequestInit).method !== 'GET')).toBe(false)
    // Read-only: the only thing stored is the shared current board, never a run pin.
    expect(Object.keys(window.localStorage)).toEqual(['archon.currentBoard.v1'])
  })

  it('names why the mission run is blocked from its run evidence', async () => {
    const board = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/runs?mission=mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: [{ ...runStatus('run_01D_blocked', 'mission-alpha'), status: 'blocked', resumeAllowed: false }] }))
      }
      if (url === '/api/formations/runs/run_01D_blocked/events') {
        return Promise.resolve(jsonResponse({ success: true, data: { events: [{ seq: 7, type: 'run_blocked', nodeId: 'human-review', gateId: 'human-review' }] } }))
      }
      if (url === '/api/formations/runs/run_01D_blocked/evidence/problems') {
        return Promise.resolve(jsonResponse({ success: true, data: { problems: [{ seq: 7, type: 'run_blocked', nodeIds: ['human-review'], reason: { text: 'invalid judge result: expected exactly one chrote-verdict block', bytes: 64 }, resumeAllowed: false }] } }))
      }
      if (url === '/api/formations/runs?mission=empty') return Promise.resolve(jsonResponse({ success: true, data: [] }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    const run = await screen.findByTestId('mission-run')
    await waitFor(() => expect(run).toHaveTextContent('blocked: invalid judge result: expected exactly one chrote-verdict block'))
    expect(within(run).getByRole('link', { name: 'Open on Missions' })).toHaveAttribute('href', '?mission=mission-board&run=run_01D_blocked')
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
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName, summary: 'Designs things', harnessVariants: [claudeVariant({ sessionStem: 'susie', launch: 'claude', model: 'claude-opus-5' })] }) }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/empty') {
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
    // No inert launch field: the legacy string is explained, and the seat command is the daemon's.
    expect(within(editor).queryByLabelText('Agent launch command')).toBeNull()
    expect(within(editor).getByText(/legacy launch string/)).toHaveTextContent('Seats do not use it')
    expect(within(editor).getByTestId('seat-launch-claude-code')).toHaveTextContent("exec '/usr/bin/claude' --model 'claude-opus-5' --effort 'medium'")
    expect(within(editor).getByLabelText('claude-code model')).toHaveValue('claude-opus-5')
    expect(within(editor).getByLabelText('claude-code effort')).toHaveDisplayValue('medium (default)')
    expect(within(within(editor).getByLabelText('claude-code effort')).queryByRole('option', { name: 'ultra' })).toBeNull()
    fireEvent.change(name, { target: { value: 'Susie Designer' } })
    fireEvent.change(within(editor).getByLabelText('claude-code effort'), { target: { value: 'xhigh' } })
    fireEvent.click(within(editor).getByRole('button', { name: 'Save agent override' }))

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('susie-etag')
    expect(patches[0].body).toMatchObject({ displayName: 'Susie Designer', summary: 'Designs things', variants: [{ id: 'claude-code', effort: 'xhigh' }] })
    expect(patches[0].body).not.toHaveProperty('launch')
    await waitFor(() => expect(screen.queryByTestId('persona-editor')).toBeNull())
    expect(await screen.findByRole('button', { name: /inspect Susie Designer/i })).toBeInTheDocument()
  })

  it('shows and edits each variant\'s model and effort in the inspector, and the command seats run', async () => {
    const board = emptyBoard()
    const patches: unknown[] = []
    let variant = claudeVariant({ sessionStem: 'critic' })
    const codex = { id: 'openai-codex', sessionStem: 'codex-critic', model: 'gpt-6-sol', effort: 'ultra', effectiveEffort: 'ultra', efforts: HARNESSES[1].efforts, seatLaunch: "exec '/usr/bin/codex' --model 'gpt-6-sol' -c 'model_reasoning_effort=\"ultra\"'" }
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('critic', { displayName: 'Critic', harnessDefault: 'claude-code' })], count: 1, harnesses: HARNESSES } }))
      if (url === '/api/agents/critic' && init?.method === 'PATCH') {
        const body = JSON.parse(String(init.body))
        patches.push(body)
        if (headerValue(init.headers, 'If-Match') !== 'critic-etag') return Promise.resolve(jsonResponse({ success: false, error: { message: 'Agent card changed; reload and retry' } }, 409))
        variant = claudeVariant({ sessionStem: 'critic', model: body.variants[0].model, effort: body.variants[0].effort })
        return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { harnessVariants: [variant, codex] }) }, 200, { ETag: 'critic-etag-2' }))
      }
      if (url === '/api/agents/critic') return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { displayName: 'Critic', harnessVariants: [variant, codex] }) }, 200, { ETag: 'critic-etag' }))
      if (url === '/api/formations/missions') return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      if (url === '/api/formations/missions/empty/layout') return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      if (url === '/api/formations/missions/empty') return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    const claude = await within(inspector).findByRole('form', { name: 'claude-code harness variant' })
    expect(within(inspector).getByText('harness default · effort medium (default)')).toBeInTheDocument()
    expect(within(claude).getByLabelText('claude-code model')).toHaveValue('')
    expect(within(claude).getByLabelText('claude-code model')).toHaveAttribute('placeholder', 'harness default')
    expect(within(claude).getByLabelText('claude-code effort')).toHaveDisplayValue('medium (default)')
    expect(within(claude).getByTestId('seat-launch-claude-code')).toHaveTextContent("exec '/usr/bin/claude' --effort 'medium' --dangerously-skip-permissions")
    const codexForm = within(inspector).getByRole('form', { name: 'openai-codex harness variant' })
    expect(within(codexForm).getByLabelText('openai-codex model')).toHaveValue('gpt-6-sol')
    expect(within(codexForm).getByLabelText('openai-codex effort')).toHaveDisplayValue('ultra')
    expect(within(claude).getByRole('button', { name: 'Save claude-code model and effort' })).toBeDisabled()

    fireEvent.change(within(claude).getByLabelText('claude-code model'), { target: { value: 'claude-opus-5' } })
    fireEvent.change(within(claude).getByLabelText('claude-code effort'), { target: { value: 'low' } })
    fireEvent.click(within(claude).getByRole('button', { name: 'Save claude-code model and effort' }))

    await waitFor(() => expect(within(claude).getByTestId('seat-launch-claude-code')).toHaveTextContent("--model 'claude-opus-5' --effort 'low'"))
    expect(patches).toEqual([{ variants: [{ id: 'claude-code', model: 'claude-opus-5', effort: 'low' }] }])
    expect(within(claude).getByRole('status')).toHaveTextContent('Saved')
    expect(within(inspector).getByText('claude-opus-5 · effort low')).toBeInTheDocument()

    // A stale card is reloaded and the operator's draft is kept to save again.
    fireEvent.change(within(claude).getByLabelText('claude-code effort'), { target: { value: 'max' } })
    fireEvent.click(within(claude).getByRole('button', { name: 'Save claude-code model and effort' }))
    expect(await within(claude).findByRole('alert')).toHaveTextContent('changed elsewhere and was reloaded')
    expect(within(claude).getByLabelText('claude-code effort')).toHaveDisplayValue('max')
  })

  it('offers a board retry when the selected board fails to load', async () => {
    const board = emptyBoard()
    let failBoard = true
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      }
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/missions/empty/layout') {
        if (failBoard) return Promise.reject(new Error('layout unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/empty') {
        if (failBoard) return Promise.reject(new Error('board unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText(/Mission load failed:/)).toBeInTheDocument()
    failBoard = false
    fireEvent.click(screen.getByRole('button', { name: /retry board/i }))

    expect(await screen.findByText(/No personas yet/)).toBeInTheDocument()
    expect(screen.queryByText(/Mission load failed:/)).not.toBeInTheDocument()
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/formations/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/mission-board' && init?.method === 'PATCH') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/formations/missions/mission-board') {
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/empty') {
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
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0, harnesses: HARNESSES } }))
      }
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /new agent/i }))
    expect(screen.queryByLabelText('Launch')).toBeNull()
    fireEvent.change(screen.getByLabelText('Harness'), { target: { value: 'hermes' } })
    expect(screen.queryByLabelText('Model')).toBeNull()
    expect(screen.getByText(/Archon cannot start hermes seats, so it takes no model or effort/)).toBeInTheDocument()
    // hermes keeps its launch command: archon agent spawn runs it. A launchable harness never sends one.
    fireEvent.change(screen.getByLabelText('Launch command (archon agent spawn)'), { target: { value: 'hermes --profile writer' } })
    expect(screen.queryByRole('option', { name: 'codex' })).toBeNull()
    fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'writer' } })
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Writer' } })
    fireEvent.change(screen.getByLabelText('Harness'), { target: { value: 'openai-codex' } })
    fireEvent.change(screen.getByLabelText('Summary'), { target: { value: 'Writes launch copy' } })
    expect(screen.getByLabelText('Model')).toHaveAttribute('placeholder', 'harness default')
    expect(screen.getByLabelText('Effort')).toHaveDisplayValue('medium (default)')
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: ' gpt-6-sol ' } })
    fireEvent.change(screen.getByLabelText('Effort'), { target: { value: 'ultra' } })
    fireEvent.change(screen.getByLabelText('Capabilities'), { target: { value: 'writing, voice' } })
    fireEvent.click(screen.getByRole('button', { name: /^Create persona$/i }))

    await waitFor(() => expect(postedBodies).toHaveLength(1))
    expect(postedBodies[0]).toMatchObject({
      id: 'writer',
      displayName: 'Writer',
      kind: 'specialist',
      harness: 'openai-codex',
      summary: 'Writes launch copy',
      model: 'gpt-6-sol',
      effort: 'ultra',
      capabilities: ['writing', 'voice'],
    })
    expect(postedBodies[0]).not.toHaveProperty('launch')
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
      if (url === '/api/formations/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { boards: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/formations/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/formations/missions/empty') {
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

/** Mission → Build → Tool → Review gate; the gate is judged by a two-formation chain. */
function judgedBoard(): BoardDocument {
  const solo = (id: string, title: string, agentId?: string) => ({
    id, type: 'solo' as const, title,
    inputs: [{ id: 'in', label: 'Input' }],
    outputs: [{ id: 'out', label: 'Output' }],
    slots: [{ id: 'agent', label: 'Agent', controller: true, harness: 'claude-code', ...(agentId ? { agentId } : {}) }],
  })
  return {
    id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag',
    missions: [{ id: 'mission', title: 'Mission', goal: 'Ship it' }],
    formations: [solo('build', 'Build', 'builder'), solo('judge-a', 'First judge', 'critic'), solo('judge-b', 'Second judge'), solo('ship', 'Ship', 'builder')],
    tools: [{ id: 'lint', title: 'Lint', profileId: 'shell', profileVersion: '1', params: {}, inputs: [{ id: 'in' }], outputs: [{ id: 'out' }] }],
    gates: [{ id: 'review', title: 'Review', kinds: ['formation'], criterion: 'The judges accept it.' }],
    connections: [
      { id: 'c1', from: 'mission:out', to: 'build:in' },
      { id: 'c2', from: 'build:out', to: 'lint:in' },
      { id: 'c3', from: 'lint:out', to: 'review:in' },
      { id: 'c4', from: 'review:judge', to: 'judge-a:in' },
      { id: 'c5', from: 'judge-a:out', to: 'judge-b:in' },
      { id: 'c6', from: 'judge-b:out', to: 'review:judge' },
      { id: 'c7', from: 'review:pass', to: 'ship:in' },
    ],
  } as BoardDocument
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

/** The daemon's launchable harnesses, as GET /api/agents lists them. */
const HARNESSES = [
  { id: 'claude-code', executable: 'claude', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium' },
  { id: 'openai-codex', executable: 'codex', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], defaultEffort: 'medium' },
]

/** A claude-code variant as the daemon reads it, with the derived seat launch. */
function claudeVariant(overrides: { sessionStem?: string; launch?: string; model?: string; effort?: string } = {}) {
  const effort = overrides.effort || 'medium'
  return {
    id: 'claude-code',
    ...overrides,
    effectiveEffort: effort,
    efforts: HARNESSES[0].efforts,
    seatLaunch: `exec '/usr/bin/claude'${overrides.model ? ` --model '${overrides.model}'` : ''} --effort '${effort}' --dangerously-skip-permissions`,
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
