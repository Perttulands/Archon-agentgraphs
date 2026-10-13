import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsView from './AgentsView'

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

  it('lists a role\'s slots in every mission, and a use opens its mission', async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('critic', { displayName: 'Critic' }), agent('builder', { displayName: 'Builder' })], count: 2 } }))
      }
      if (url === '/api/agents/builder') return Promise.resolve(jsonResponse({ success: true, data: persona('builder', { displayName: 'Builder' }) }, 200, { ETag: 'builder-etag' }))
      if (url === '/api/agents/builder/usage') return Promise.resolve(jsonResponse({ success: true, data: { usage: [] } }))
      if (url === '/api/agents/critic') return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { displayName: 'Critic' }) }, 200, { ETag: 'critic-etag' }))
      if (url === '/api/agents/critic/usage') {
        return Promise.resolve(jsonResponse({ success: true, data: { usage: [
          { missionId: 'judged', missionSlug: 'judged', missionTitle: 'Judged', formationId: 'judge-a', formationTitle: 'First judge', slotId: 'agent', slotLabel: 'Agent' },
          { missionId: 'other', missionSlug: 'other', missionTitle: 'Other', formationId: 'check', formationTitle: 'Check', slotId: 'agent', slotLabel: 'Checker' },
        ] } }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })
    const onOpenMission = vi.fn()

    render(<AgentsView onOpenMission={onOpenMission} />)

    // The view is the library alone: it opens on a role and draws no mission.
    const detail = await screen.findByRole('main', { name: 'Role' })
    expect(await within(detail).findByText('No slot uses this role.')).toBeInTheDocument()
    expect(within(screen.getByRole('complementary', { name: 'Agent roster' })).getByText('2 roles')).toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Mission' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Inspect Critic' }))
    const usedBy = await within(detail).findByRole('region', { name: 'Used by' })
    await waitFor(() => expect(Array.from(usedBy.querySelectorAll('.agx-role-use')).map(row => row.textContent)).toEqual(['Judged › First judge › Agent', 'Other › Check › Checker']))
    expect(within(detail).getByText('in 2 slots')).toBeInTheDocument()
    fireEvent.click(within(usedBy).getByRole('button', { name: /Other › Check/ }))
    expect(onOpenMission).toHaveBeenCalledWith('other')
  })

  it('edits a persona from the Agents tab with the shared persona editor', async () => {
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
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: /inspect Susie/i }))
    fireEvent.click(await screen.findByRole('button', { name: 'Edit role' }))
    const editor = await screen.findByTestId('persona-editor')
    const name = await within(editor).findByLabelText('Role display name')
    expect(within(editor).getByLabelText('Role summary')).toHaveValue('Designs things')
    // A role is role text: no launch, model or effort to edit.
    expect(within(editor).queryByLabelText('Agent launch command')).toBeNull()
    expect(within(editor).queryByText(/model|effort|harness variant/i)).toBeNull()
    fireEvent.change(name, { target: { value: 'Susie Designer' } })
    fireEvent.click(within(editor).getByRole('button', { name: 'Save role' }))

    await waitFor(() => expect(patches).toHaveLength(1))
    expect(headerValue(patches[0].headers, 'If-Match')).toBe('susie-etag')
    expect(patches[0].body).toEqual({ displayName: 'Susie Designer', kind: 'specialist', summary: 'Designs things', capabilities: [] })
    await waitFor(() => expect(screen.queryByTestId('persona-editor')).toBeNull())
    expect(await screen.findByRole('button', { name: /inspect Susie Designer/i })).toBeInTheDocument()
  })

  it('shows a role as role text, with no model or effort of its own', async () => {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: [agent('critic', { displayName: 'Critic' })], count: 1, harnesses: HARNESSES } }))
      if (url === '/api/agents/critic') return Promise.resolve(jsonResponse({ success: true, data: persona('critic', { displayName: 'Critic', summary: 'Reviews the brief.', notes: [{ ts: '2026-10-02T20:00:00Z', actor: 'operator', text: 'Read /tmp/brief.md for archon-o7p.13.2' }] }) }, 200, { ETag: 'critic-etag' }))
      if (url === '/api/agents/critic/usage') return Promise.resolve(jsonResponse({ success: true, data: { usage: [{ missionId: 'empty', missionSlug: 'empty', missionTitle: 'Empty', formationId: 'review', formationTitle: 'Review', slotId: 'reviewer', slotLabel: 'Reviewer' }] } }))
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('main', { name: 'Role' })
    expect(await within(inspector).findByText('Reviews the brief.')).toBeInTheDocument()
    expect(within(inspector).getByRole('button', { name: '/tmp/brief.md' })).toBeInTheDocument()
    expect(within(inspector).getByRole('button', { name: 'archon-o7p.13.2' })).toBeInTheDocument()
    expect(await within(inspector).findByText('in 1 slot')).toBeInTheDocument()
    expect(within(inspector).queryByText(/harness variants|^Runs$|starts as/i)).toBeNull()
    expect(within(inspector).queryByRole('textbox', { name: /model/i })).toBeNull()
    expect(within(inspector).queryByRole('combobox', { name: /effort/i })).toBeNull()
  })

  it('retires, brings back and deletes a role only after saying what that does to the slots that use it', async () => {
    const cards: Record<string, Record<string, unknown>> = {
      critic: persona('critic', { displayName: 'Critic', kind: 'reviewer' }),
      spare: persona('spare', { displayName: 'Spare' }),
      judge: persona('judge', { displayName: 'Judge', kind: 'judge', preset: true }),
    }
    const uses: Record<string, unknown[]> = {
      critic: [{ missionId: 'other', missionSlug: 'other', missionTitle: 'Other', formationId: 'check', formationTitle: 'Check', slotId: 'agent', slotLabel: 'Checker' }],
      spare: [],
      judge: [],
    }
    const writes: Array<{ method: string; url: string; body: unknown; ifMatch: string }> = []
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method || 'GET'
      if (url === '/api/agents') return Promise.resolve(jsonResponse({ success: true, data: { agents: Object.values(cards).map(card => agent(String(card.id), { displayName: card.displayName })), count: 3 } }))
      const usage = url.match(/^\/api\/agents\/([^/]+)\/usage$/)
      if (usage) return Promise.resolve(jsonResponse({ success: true, data: { usage: uses[usage[1]] } }))
      const role = url.match(/^\/api\/agents\/([^/]+)$/)
      if (role) {
        const id = role[1]
        if (method !== 'GET') writes.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : null, ifMatch: headerValue(init?.headers, 'If-Match') || '' })
        if (method === 'PATCH') cards[id] = { ...cards[id], status: (JSON.parse(String(init?.body)) as { retire: boolean }).retire ? 'retired' : 'active' }
        if (method === 'DELETE') {
          delete cards[id]
          return Promise.resolve(jsonResponse({ success: true, data: { deleted: id, builtinRemains: false } }))
        }
        return Promise.resolve(jsonResponse({ success: true, data: cards[id] }, 200, { ETag: `${id}-etag` }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect Critic' }))
    const inspector = await screen.findByRole('main', { name: 'Role' })
    expect(await within(inspector).findByRole('button', { name: 'Other › Check › Checker' })).toBeInTheDocument()

    // Retire says the slot stops running, and only the confirmation writes.
    fireEvent.click(within(inspector).getByRole('button', { name: 'Retire' }))
    const retire = within(inspector).getByRole('group', { name: 'Retire Critic' })
    expect(retire).toHaveTextContent('Retire Critic? The slot above stops running until it has another role or none.')
    expect(writes).toHaveLength(0)
    fireEvent.click(within(retire).getByRole('button', { name: 'Retire Critic' }))
    await waitFor(() => expect(writes).toEqual([{ method: 'PATCH', url: '/api/agents/critic', body: { retire: true }, ifMatch: 'critic-etag' }]))
    fireEvent.click(await within(inspector).findByRole('button', { name: 'Bring back' }))
    await waitFor(() => expect(writes[1]).toEqual({ method: 'PATCH', url: '/api/agents/critic', body: { retire: false }, ifMatch: 'critic-etag' }))

    // A role a slot uses is not deleted: the view says what to do instead.
    fireEvent.click(await within(inspector).findByRole('button', { name: 'Delete' }))
    const refused = within(inspector).getByRole('group', { name: 'Delete Critic' })
    expect(refused).toHaveTextContent('Critic staffs the 1 slot above. Give it another role or none before deleting Critic, or retire Critic instead.')
    expect(within(refused).queryByRole('button', { name: 'Delete Critic' })).toBeNull()
    fireEvent.click(within(refused).getByRole('button', { name: 'Close' }))

    // A built-in role offers no Delete.
    fireEvent.click(screen.getByRole('button', { name: 'Inspect Judge' }))
    expect(await within(inspector).findByText('No slot uses this role.')).toBeInTheDocument()
    expect(within(inspector).queryByRole('button', { name: 'Delete' })).toBeNull()

    // An unused role is deleted after one confirmation.
    fireEvent.click(screen.getByRole('button', { name: 'Inspect Spare' }))
    expect(await within(inspector).findByText('No slot uses this role.')).toBeInTheDocument()
    fireEvent.click(within(inspector).getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(within(inspector).getByRole('group', { name: 'Delete Spare' })).getByRole('button', { name: 'Delete Spare' }))
    await waitFor(() => expect(writes[writes.length - 1]).toEqual({ method: 'DELETE', url: '/api/agents/spare', body: null, ifMatch: 'spare-etag' }))
    // The deleted role leaves the page, which shows another role.
    await waitFor(() => expect(within(inspector).queryByRole('group', { name: 'Delete Spare' })).toBeNull())
  })

  it('lists roles by name, not by harness, and states only what differs from offline', async () => {
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
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText('Attached Live')).toBeInTheDocument()
    const roster = screen.getByRole('complementary', { name: 'Agent roster' })
    expect(Array.from(roster.querySelectorAll('.roster-group-label')).map(label => label.textContent)).toEqual(['Roles', 'Unbound'])
    // Roles carry no harness: no harness mark, and alphabetical whatever the default harness.
    expect(Array.from(roster.querySelectorAll('.roster-group[data-provider="roles"] .ragent .n')).map(name => name.textContent)).toEqual(['Ambiguous One', 'Attached Live', 'Offline One', 'Retired One'])
    const codexRow = within(roster).getByRole('button', { name: /inspect Attached Live/i })
    expect(codexRow.querySelector('.av svg')).toBeNull()
    expect(within(codexRow).getByText('AT')).toBeInTheDocument()

    expect(within(roster).getAllByText('live')).toHaveLength(2)
    expect(within(roster).getByText('ambiguous')).toBeInTheDocument()
    expect(within(roster).queryByText('offline')).not.toBeInTheDocument()
    expect(within(roster).getByText('attached')).toBeInTheDocument()
    expect(within(roster).getByText('not assignable')).toBeInTheDocument()
    expect(within(roster).getByText('no role')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /inspect Retired One/i }))
    const inspector = await screen.findByRole('main', { name: 'Role' })
    await waitFor(() => expect(within(inspector).getByText('retired')).toBeInTheDocument())
    expect(within(inspector).getByText('offline')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /inspect floating-session/i }))
    fireEvent.click(screen.getByRole('button', { name: /make a role from this session/i }))
    // The role is named after the session, which is then the role's own.
    expect(screen.getByLabelText('Role id')).toHaveValue('floating-session')
    expect(screen.queryByLabelText('Session stem')).toBeNull()
  })

  it('creates a role from role text alone', async () => {
    const postedBodies: unknown[] = []
    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/agents' && init?.method === 'POST') {
        postedBodies.push(JSON.parse(String(init.body)))
        return Promise.resolve(jsonResponse({ success: true, data: persona('writer', { displayName: 'Writer' }) }, 201, { ETag: 'writer-etag' }))
      }
      if (url === '/api/agents') {
        return Promise.resolve(jsonResponse({ success: true, data: { agents: [], count: 0, harnesses: HARNESSES } }))
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`))
    })

    render(<AgentsView />)

    expect(await screen.findByText(/No roles yet/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /new role/i }))
    // A new role asks only for role text: no launch, harness, model or effort.
    const form = screen.getByRole('dialog', { name: 'New role' })
    expect(within(form).queryByLabelText('Launch')).toBeNull()
    expect(within(form).queryByLabelText('Harness')).toBeNull()
    expect(within(form).queryByLabelText('Model')).toBeNull()
    expect(within(form).queryByLabelText('Effort')).toBeNull()
    expect(within(form).queryByText(/model|effort/i)).toBeNull()
    fireEvent.change(screen.getByLabelText('Role id'), { target: { value: 'INVALID ROLE' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Create role' }))
    expect(within(form).getByRole('alert')).toHaveTextContent('Use lowercase letters')
    expect(postedBodies).toHaveLength(0)
    fireEvent.click(within(form).getByRole('button', { name: 'Dismiss' }))
    expect(within(form).queryByRole('alert')).toBeNull()
    fireEvent.change(screen.getByLabelText('Role id'), { target: { value: 'writer' } })
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Writer' } })
    fireEvent.change(screen.getByLabelText('Summary'), { target: { value: 'Writes launch copy' } })
    fireEvent.change(screen.getByLabelText('Capabilities'), { target: { value: 'writing, voice' } })
    fireEvent.click(screen.getByRole('button', { name: /^Create role$/i }))

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

/** Mission → Build → Tool → Review gate; the gate is judged by a two-formation chain. */
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
