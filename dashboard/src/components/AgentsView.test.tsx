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
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag' }] } }))
      }
      if (url === '/api/missions/judged/layout') return Promise.resolve(jsonResponse({ success: true, data: { layout: { missionId: 'judged', missionRev: 3, etag: 'l', nodes: [] } } }, 200, { ETag: 'l' }))
      if (url === '/api/missions/judged') return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'judged-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Second judge')).toBeInTheDocument()
    expect(screen.getByText('First judge')).toBeInTheDocument()
    expect(screen.getAllByText('judges Review')).toHaveLength(2)
    expect(screen.getByText('judged by First judge → Second judge')).toBeInTheDocument()
    expect(screen.getByText('3/4 slots staffed · 1 open')).toBeInTheDocument()
    expect(screen.queryByText(/ready/)).not.toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'Agent roster' })).getByText('2 roles · 2 in use')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    expect(within(inspector).getByText(/^First judge/)).toBeInTheDocument()
    expect(within(inspector).queryByText('No slots on this mission.')).not.toBeInTheDocument()
  })

  it('opens the shared current board and makes a board chosen here current for Boards too', async () => {
    const judged = judgedBoard()
    const mission = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [
          { id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' },
          { id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag' },
        ] } }))
      }
      if (url.endsWith('/layout')) return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'l' }))
      if (url === '/api/missions/judged') return Promise.resolve(jsonResponse({ success: true, data: { mission: judged } }, 200, { ETag: 'judged-etag' }))
      if (url === '/api/missions/mission-board') return Promise.resolve(jsonResponse({ success: true, data: { mission: mission } }, 200, { ETag: 'board-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })
    window.localStorage.setItem('archon.currentMission.v1', 'judged')

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
    expect(window.localStorage.getItem('archon.currentMission.v1')).toBe('mission-board')
  })

  it('orders staffing by the wiring from the mission, using the canvas only between parallel branches', () => {
    const board = missionBoard()
    const layout: LayoutDocument = {
      missionId: 'board-1', missionRev: 7, etag: 'layout-etag',
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

  it('staffs a slot through its sentence and empties one on purpose, each stating the slot in full', async () => {
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
              agent('coder', { displayName: 'Coder', liveness: 'live', kind: 'builder' }),
              agent('susie', { displayName: 'Susie', tags: ['design'], liveness: 'offline' }),
            ],
            count: 2,
            harnesses: HARNESSES,
            effortPolicy: [{ effort: 'low', use: 'errands' }, { effort: 'medium', use: 'making things' }],
          },
        }))
      }
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/mission-board' && init?.method === 'PATCH') {
        patches.push({ headers: init.headers, body: JSON.parse(String(init.body)) })
        return Promise.resolve(jsonResponse({ success: true, data: { mission: { ...board, etag: 'board-etag-2' } } }, 200, { ETag: 'board-etag-2' }))
      }
      if (url === '/api/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'board-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Authoring')).toBeInTheDocument()
    expect(screen.getByText('Fix Pass')).toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'Agent roster' })).getByText('2 roles · 2 in use · 1 live')).toBeInTheDocument()
    expect(screen.getByText('Escalate Fail')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Review: not staffed' }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    // No list of eligible agents: the slot says what it runs, and its words staff it.
    expect(within(inspector).queryByText('Eligible agents')).not.toBeInTheDocument()
    fireEvent.click(within(inspector).getByRole('button', { name: 'Staff Review' }))
    const sentence = await screen.findByRole('dialog', { name: 'Staff Review' })
    fireEvent.change(within(sentence).getByRole('textbox'), { target: { value: 'coder' } })
    fireEvent.keyDown(sentence, { key: 'Enter' })

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('board-etag')
    expect(patches[0].body).toMatchObject({
      expectedRev: 7,
      updatedBy: 'human:ui',
      assignSlot: { formationId: 'authoring', slotId: 'reviewer', agentId: 'coder', harness: 'claude-code', model: 'opus', effort: 'medium' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Lead (controller): Susie on Claude Code · default model · medium' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Empty Lead' }))

    await waitFor(() => expect(patches).toHaveLength(2))
    expect(patches[1].body).toMatchObject({
      expectedRev: 7,
      updatedBy: 'human:ui',
      assignSlot: { formationId: 'authoring', slotId: 'lead', agentId: '', harness: '', model: '', effort: '' },
    })
  })

  it('names a gate checker in operator words instead of the internal kind', async () => {
    const board = { ...missionBoard(), gates: [{ id: 'human-review', title: 'Human Review', kinds: ['formation', 'human'], criterion: 'Approve the branch.' }] }
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'board-etag' }))
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
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/runs?mission=mission-board') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: [
            { ...runStatus('run_01A_old', 'mission-alpha'), status: 'succeeded', final: true },
            { ...runStatus('run_01B_cli', 'mission-alpha'), status: 'waiting_human', waitingGates: [{ gateId: 'human-review', requestedSeq: 4 }] },
            { ...runStatus('run_01C_other', 'mission-beta'), status: 'running' },
          ],
        }))
      }
      if (url === '/api/runs/run_01B_cli/events') {
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
    expect(Object.keys(window.localStorage)).toEqual(['archon.currentMission.v1'])
  })

  it('names why the mission run is blocked from its run evidence', async () => {
    const board = missionBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'board-1', slug: 'mission-board', title: 'Mission Board', rev: 7, etag: 'board-etag' }] } }))
      }
      if (url === '/api/missions/mission-board/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: missionLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'board-etag' }))
      }
      if (url === '/api/runs?mission=mission-board') {
        return Promise.resolve(jsonResponse({ success: true, data: [{ ...runStatus('run_01D_blocked', 'mission-alpha'), status: 'blocked', resumeAllowed: false }] }))
      }
      if (url === '/api/runs/run_01D_blocked/events') {
        return Promise.resolve(jsonResponse({ success: true, data: { events: [{ seq: 7, type: 'run_blocked', nodeId: 'human-review', gateId: 'human-review' }] } }))
      }
      if (url === '/api/runs/run_01D_blocked/evidence/problems') {
        return Promise.resolve(jsonResponse({ success: true, data: { problems: [{ seq: 7, type: 'run_blocked', nodeIds: ['human-review'], reason: { text: 'invalid judge result: expected exactly one archon-verdict block', bytes: 64 }, resumeAllowed: false }] } }))
      }
      if (url === '/api/runs?mission=empty') return Promise.resolve(jsonResponse({ success: true, data: [] }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    const run = await screen.findByTestId('mission-run')
    await waitFor(() => expect(run).toHaveTextContent('blocked: invalid judge result: expected exactly one archon-verdict block'))
    expect(within(run).getByRole('link', { name: 'Open on Missions' })).toHaveAttribute('href', '?mission=mission-board&run=run_01D_blocked')
  })

  it('edits a persona from the Agents tab with the shared persona editor', async () => {
    const board = emptyBoard()
    const patches: Array<{ headers: HeadersInit | undefined; body: unknown }> = []
    let displayName = 'Susie'
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('susie', { displayName })], count: 1 } }))
      if (url === '/api/agents/susie' && init?.method === 'PATCH') {
        const body = JSON.parse(String(init.body))
        patches.push({ headers: init.headers, body })
        displayName = body.displayName
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName }) }, 200, { ETag: 'susie-etag-2' }))
      }
      if (url === '/api/agents/susie') {
        return Promise.resolve(jsonResponse({ success: true, data: persona('susie', { displayName, summary: 'Designs things' }) }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /inspect Susie/i }))
    fireEvent.click(await screen.findByRole('button', { name: 'Edit persona' }))
    const editor = await screen.findByTestId('persona-editor')
    const name = await within(editor).findByLabelText('Agent display name')
    expect(within(editor).getByLabelText('Agent summary')).toHaveValue('Designs things')
    // A role is role text: no launch, model or effort to edit.
    expect(within(editor).queryByLabelText('Agent launch command')).toBeNull()
    expect(within(editor).queryByText(/model|effort|harness variant/i)).toBeNull()
    fireEvent.change(name, { target: { value: 'Susie Designer' } })
    fireEvent.click(within(editor).getByRole('button', { name: 'Save agent override' }))

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('susie-etag')
    expect(patches[0].body).toEqual({ displayName: 'Susie Designer', kind: 'specialist', summary: 'Designs things', capabilities: [] })
    await waitFor(() => expect(screen.queryByTestId('persona-editor')).toBeNull())
    expect(await screen.findByRole('button', { name: /inspect Susie Designer/i })).toBeInTheDocument()
  })

  it('shows a role as role text, with what each slot it staffs runs', async () => {
    const board = emptyBoard()
    board.formations = [{ id: 'review', type: 'solo', title: 'Review', inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Output' }],
      slots: [{ id: 'reviewer', label: 'Reviewer', controller: true, agentId: 'critic', harness: 'openai-codex', model: 'gpt-6-astra', effort: 'xhigh' }] }]
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('critic', { displayName: 'Critic' })], count: 1, harnesses: HARNESSES } }))
      if (url === '/api/agents/critic') return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { displayName: 'Critic', summary: 'Reviews the brief.' }) }, 200, { ETag: 'critic-etag' }))
      if (url === '/api/missions') return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      if (url === '/api/missions/empty/layout') return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      if (url === '/api/missions/empty') return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('complementary', { name: 'Inspector' })
    expect(await within(inspector).findByText('Reviews the brief.')).toBeInTheDocument()
    expect(within(inspector).getByText('in 1 slot')).toBeInTheDocument()
    expect(within(inspector).getByText('Codex · gpt-6-astra · xhigh')).toBeInTheDocument()
    expect(within(inspector).queryByText(/harness variants|^Runs$|starts as/i)).toBeNull()
    expect(within(inspector).queryByRole('textbox', { name: /model/i })).toBeNull()
    expect(within(inspector).queryByRole('combobox', { name: /effort/i })).toBeNull()
  })

  it('offers a board retry when the selected board fails to load', async () => {
    const board = emptyBoard()
    let failBoard = true
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0 } }))
      }
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/missions/empty/layout') {
        if (failBoard) return Promise.reject(new Error('layout unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/empty') {
        if (failBoard) return Promise.reject(new Error('board unavailable'))
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
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

  it('lists roles by name, not by harness, and states only what differs from offline', async () => {
    const board = emptyBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({
          success: true,
          data: {
            agents: [
              agent('attached-live', { displayName: 'Attached Live', liveness: 'live', attached: true }),
              agent('ambiguous-one', { displayName: 'Ambiguous One', liveness: 'ambiguous' }),
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
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Attached Live')).toBeInTheDocument()
    const roster = screen.getByRole('complementary', { name: 'Agent roster' })
    expect(Array.from(roster.querySelectorAll('.roster-group-label')).map(label => label.textContent)).toEqual(['Roles', 'Unbound'])
    // Roles carry no harness: no harness mark, and alphabetical whatever the default harness.
    expect(Array.from(roster.querySelectorAll('.roster-group:first-child .ragent .n')).map(name => name.textContent)).toEqual(['Ambiguous One', 'Attached Live', 'Offline One', 'Retired One'])
    const codexRow = within(roster).getByRole('button', { name: /inspect Attached Live/i })
    expect(codexRow.querySelector('.av svg')).toBeNull()
    expect(within(codexRow).getByText('AT')).toBeInTheDocument()

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
    // The role is named after the session, which is then the role's own.
    expect(screen.getByLabelText('Agent id')).toHaveValue('floating-session')
    expect(screen.queryByLabelText('Session stem')).toBeNull()
  })

  it('creates a role from role text alone', async () => {
    const postedBodies: unknown[] = []
    const board = emptyBoard()
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents' && init?.method === 'POST') {
        postedBodies.push(JSON.parse(String(init.body)))
        return Promise.resolve(jsonResponse({ success: true, data: persona('writer', { displayName: 'Writer' }) }, 201, { ETag: 'writer-etag' }))
      }
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0, harnesses: HARNESSES } }))
      }
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /new agent/i }))
    // A new role asks only for role text: no launch, harness, model or effort.
    const form = screen.getByRole('dialog', { name: 'Create persona' })
    expect(within(form).queryByLabelText('Launch')).toBeNull()
    expect(within(form).queryByLabelText('Harness')).toBeNull()
    expect(within(form).queryByLabelText('Model')).toBeNull()
    expect(within(form).queryByLabelText('Effort')).toBeNull()
    expect(within(form).queryByText(/model|effort/i)).toBeNull()
    fireEvent.change(screen.getByLabelText('Agent id'), { target: { value: 'writer' } })
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Writer' } })
    fireEvent.change(screen.getByLabelText('Summary'), { target: { value: 'Writes launch copy' } })
    fireEvent.change(screen.getByLabelText('Capabilities'), { target: { value: 'writing, voice' } })
    fireEvent.click(screen.getByRole('button', { name: /^Create persona$/i }))

    await waitFor(() => expect(postedBodies).toHaveLength(1))
    expect(postedBodies[0]).toMatchObject({
      id: 'writer',
      displayName: 'Writer',
      kind: 'specialist',
      summary: 'Writes launch copy',
      capabilities: ['writing', 'voice'],
    })
    expect(postedBodies[0]).not.toHaveProperty('harness')
    expect(postedBodies[0]).not.toHaveProperty('sessionStem')
    expect(postedBodies[0]).not.toHaveProperty('launch')
    expect(postedBodies[0]).not.toHaveProperty('model')
    expect(postedBodies[0]).not.toHaveProperty('effort')
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
            summary: 'Designs things',
            tags: ['design'],
            toml: 'CLAUDE.md contents',
          }),
        }, 200, { ETag: 'susie-etag' }))
      }
      if (url === '/api/missions') {
        return Promise.resolve(jsonResponse({ success: true, data: { missions: [{ id: 'empty', slug: 'empty', title: 'Empty', rev: 1, etag: 'empty-etag' }] } }))
      }
      if (url === '/api/missions/empty/layout') {
        return Promise.resolve(jsonResponse({ success: true, data: { layout: emptyLayout() } }, 200, { ETag: 'layout-etag' }))
      }
      if (url === '/api/missions/empty') {
        return Promise.resolve(jsonResponse({ success: true, data: { mission: board } }, 200, { ETag: 'empty-etag' }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /inspect Susie/i }))
    expect(await screen.findByText('Designs things')).toBeInTheDocument()
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
    inputCards: [{ id: 'mission-alpha', title: 'Mission Alpha', goal: 'Ship the redesign' }],
    formations: [
      {
        id: 'authoring',
        type: 'peer',
        title: 'Authoring',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [
          { id: 'lead', label: 'Lead', controller: true, agentId: 'susie', harness: 'claude-code', effort: 'medium' },
          { id: 'reviewer', label: 'Review', controller: false },
        ],
      },
      {
        id: 'fix-pass',
        type: 'solo',
        title: 'Fix Pass',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [{ id: 'builder', label: 'Builder', controller: true }],
      },
      {
        id: 'escalate-fail',
        type: 'solo',
        title: 'Escalate Fail',
        inputs: [{ id: 'in', label: 'Input' }],
        outputs: [{ id: 'out', label: 'Output' }],
        slots: [{ id: 'critic', label: 'Critic', controller: true, agentId: 'coder', harness: 'openai-codex', model: 'gpt-6-astra', effort: 'xhigh' }],
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
    missionId: 'board-1',
    missionRev: 7,
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
    // A staffed slot states its harness and effort; an open slot has neither.
    slots: [{ id: 'agent', label: 'Agent', controller: true, ...(agentId ? { agentId, harness: 'claude-code', effort: 'medium' } : {}) }],
  })
  return {
    id: 'judged', slug: 'judged', title: 'Judged', rev: 3, etag: 'judged-etag',
    inputCards: [{ id: 'mission', title: 'Mission', goal: 'Ship it' }],
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
    inputCards: [],
    formations: [],
    gates: [],
    connections: [],
  }
}

function emptyLayout(): LayoutDocument {
  return { missionId: 'empty', missionRev: 1, etag: 'layout-etag', nodes: [] }
}

function runStatus(runId: string, inputCardId: string) {
  return {
    runId,
    status: 'running',
    final: false,
    missionSlug: 'mission-board',
    inputCardId,
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
  { id: 'claude-code', executable: 'claude', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium', models: [{ id: 'opus' }, { id: 'sonnet' }] },
  { id: 'openai-codex', executable: 'codex', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], defaultEffort: 'medium', models: [{ id: 'gpt-6-astra' }] },
]

function persona(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    displayName: id,
    kind: 'specialist',
    summary: '',
    tags: [],
    status: '',
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
