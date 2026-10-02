import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { assignedSettings, rosterAnswer } from './roster-terms'
const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))

// Three missions for the shared current mission and a judged mission, and
// three roles, each role text only.

const ports = { inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Result' }] }
const solo = (id: string, title: string, agentId?: string, harness = 'claude-code') => ({
  id, type: 'solo', title, brief: { goal: `${title} the change.` }, ...ports,
  // A staffed slot states its harness and effort; an empty slot has neither.
  slots: [{ id: `${id}_seat`, label: title, controller: true, ...(agentId ? { agentId, harness, effort: 'medium' } : {}) }],
})
const mission = (goal: string) => ({ id: 'mission', title: 'Deliver', goal })

const boards = {
  alpha: {
    id: 'brd_alpha', slug: 'alpha', title: 'Alpha scratch', rev: 1, etag: 'alpha-1',
    inputCards: [mission('Scratch work')], formations: [solo('work', 'Work', 'builder', 'openai-codex')], gates: [], tools: [], ends: [],
    connections: [{ id: 'a1', from: 'mission:out', to: 'work:in' }],
  },
  delivery: {
    id: 'brd_delivery', slug: 'delivery', title: 'Delivery', rev: 4, etag: 'delivery-4',
    inputCards: [mission('Ship the brief')],
    formations: [solo('build', 'Build', 'builder', 'openai-codex'), solo('judge', 'Beads reviewer', 'critic'), solo('recheck', 'Second opinion'), solo('ship', 'Ship', 'builder', 'openai-codex')],
    gates: [{ id: 'review', title: 'Beads review', kinds: ['formation'], criterion: 'The Beads pass lint.' }],
    tools: [], ends: [],
    connections: [
      { id: 'd1', from: 'mission:out', to: 'build:in' },
      { id: 'd2', from: 'build:out', to: 'review:in' },
      { id: 'd3', from: 'review:judge', to: 'judge:in' },
      { id: 'd4', from: 'judge:out', to: 'recheck:in' },
      { id: 'd5', from: 'recheck:out', to: 'review:judge' },
      { id: 'd6', from: 'review:pass', to: 'ship:in' },
    ],
  },
  scouting: {
    id: 'brd_scouting', slug: 'scouting', title: 'Scouting', rev: 2, etag: 'scouting-2',
    inputCards: [mission('Map the terrain')], formations: [solo('map', 'Map', 'critic')], gates: [], tools: [], ends: [],
    connections: [{ id: 'w1', from: 'mission:out', to: 'map:in' }],
  },
}
const layouts: Record<string, Array<{ id: string; x: number; y: number }>> = {
  alpha: [{ id: 'mission', x: 100, y: 100 }, { id: 'work', x: 420, y: 100 }],
  delivery: [{ id: 'mission', x: 100, y: 100 }, { id: 'build', x: 420, y: 100 }, { id: 'review', x: 760, y: 100 },
    { id: 'judge', x: 760, y: 420 }, { id: 'recheck', x: 1080, y: 420 }, { id: 'ship', x: 1100, y: 100 }],
  scouting: [{ id: 'mission', x: 100, y: 100 }, { id: 'map', x: 420, y: 100 }],
}

// A role is role text: it names no harness, model or effort.
type Card = { id: string; displayName: string; kind: string; summary: string; tags: string[]; status?: string; preset?: boolean; notes?: Array<{ ts: string; actor: string; text: string }>; rev: number }

export async function agentsFixture(page: Page) {
  const cards: Record<string, Card> = {
    critic: { id: 'critic', displayName: 'Brief critic', kind: 'judge', summary: 'Reviews the brief.', tags: ['review'], rev: 1,
      notes: [{ ts: '2026-09-28T10:15:00Z', actor: 'agent:archon', text: 'Seeded from the catalog cleanup.' }] },
    builder: { id: 'builder', displayName: 'Builder', kind: 'builder', summary: 'Builds the change.', tags: ['implement'], rev: 1 },
    spawner: { id: 'spawner', displayName: 'Hermes spawner', kind: 'specialist', summary: 'Runs through hermes.', tags: [], rev: 1 },
  }
  const patches: unknown[] = []
  const boardPatches: unknown[] = []
  const deletions: string[] = []
  // Each test edits its own copy of the missions.
  const missions = structuredClone(boards)
  const read = (card: Card) => ({ ...card, etag: `${card.id}-${card.rev}` })

  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    const respond = (data: unknown, etag = 'fixture-etag', status = 200) => route.fulfill({ status, json: { success: true, data }, headers: { ETag: etag } })
    const fail = (status: number, code: string, message: string) => route.fulfill({ status, json: { success: false, error: { code, message } } })
    if (path === '/api/theme') return route.fulfill({ json: defaultTheme })
    if (path === '/api/missions') return respond({ missions: Object.values(missions) })
    const boardMatch = path.match(/^\/api\/missions\/([^/]+)(\/.*)?$/)
    if (boardMatch) {
      const board = missions[boardMatch[1] as keyof typeof missions] as (typeof missions)[keyof typeof missions] & { formations: Array<{ id: string; slots: Array<Record<string, unknown> & { id: string; label: string; controller: boolean }> }> }
      if (!board) return fail(404, 'NOT_FOUND', 'Mission not found')
      const rest = boardMatch[2] || ''
      if (rest === '/layout') return respond({ layout: { missionId: board.id, missionRev: board.rev, etag: `${board.slug}-layout`, nodes: layouts[board.slug], edges: [] } })
      if (rest === '/notes') return respond({ notes: { schema: 2, missionId: board.id, rev: 1, mission: [], elements: [], updatedAt: '2026-09-29T00:00:00Z', etag: 'notes-1' } })
      if (rest === '/changes') return respond({ signal: { changed: url.searchParams.get('etag') !== board.etag } })
      if (rest === '/validation') return respond({ missionRev: board.rev, missionEtag: board.etag, errors: [], warnings: [] })
      if (rest === '' && method === 'PATCH') {
        const body = route.request().postDataJSON()
        boardPatches.push({ assignSlot: body.assignSlot })
        const formation = body.assignSlot && board.formations.find((item: { id: string }) => item.id === body.assignSlot.formationId)
        const index = formation ? formation.slots.findIndex((slot: { id: string }) => slot.id === body.assignSlot.slotId) : -1
        if (!formation || index < 0) return fail(400, 'BAD_REQUEST', 'Unsupported fixture edit')
        const assigned = assignedSettings(body.assignSlot.slotId, body.assignSlot)
        if ('refused' in assigned) return fail(422, 'INVALID_SLOT_SETTINGS', assigned.refused)
        const { id, label, controller } = formation.slots[index]
        formation.slots[index] = { id, label, controller, ...assigned.settings }
        board.rev++
        board.etag = `${board.slug}-${board.rev}`
        return respond({ mission: board }, board.etag)
      }
      if (rest === '') return respond({ mission: board }, board.etag)
    }
    if (path === '/api/runs') return respond([])
    if (path === '/api/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/agents') {
      const agents = Object.values(cards).map(card => ({ id: card.id, displayName: card.displayName, kind: card.kind, summary: card.summary, tags: card.tags, liveness: 'offline', assignable: card.status !== 'retired', preset: card.preset }))
      return respond(rosterAnswer(agents))
    }
    // Usage, as the daemon reads it: every slot, in every mission, naming the role.
    const usageOf = (id: string) => Object.values(missions).flatMap(mission => mission.formations.flatMap((formation: { id: string; title: string; slots: Array<{ id: string; label: string; agentId?: string }> }) => formation.slots
      .filter(slot => slot.agentId === id)
      .map(slot => ({ missionId: mission.id, missionSlug: mission.slug, missionTitle: mission.title, formationId: formation.id, formationTitle: formation.title, slotId: slot.id, slotLabel: slot.label }))))
    const usageMatch = path.match(/^\/api\/agents\/([^/]+)\/usage$/)
    if (usageMatch) return cards[usageMatch[1]] ? respond({ usage: usageOf(usageMatch[1]) }) : fail(404, 'NOT_FOUND', 'Agent not found')
    const agentMatch = path.match(/^\/api\/agents\/([^/]+)$/)
    if (agentMatch) {
      const card = cards[agentMatch[1]]
      if (!card) return fail(404, 'NOT_FOUND', 'Agent not found')
      if (method === 'DELETE') {
        if (route.request().headers()['if-match'] !== `${card.id}-${card.rev}`) return fail(409, 'CONFLICT', 'Agent card changed; reload and retry')
        const uses = usageOf(card.id)
        if (uses.length) return fail(409, 'ROLE_IN_USE', `role "${card.id}" staffs ${uses.length} slots`)
        deletions.push(card.id)
        delete cards[card.id]
        return respond({ deleted: card.id, builtinRemains: false })
      }
      if (method === 'PATCH') {
        if (route.request().headers()['if-match'] !== `${card.id}-${card.rev}`) return fail(409, 'CONFLICT', 'Agent card changed; reload and retry')
        const body = route.request().postDataJSON()
        patches.push(body)
        const next = structuredClone(card)
        for (const key of ['displayName', 'kind', 'summary'] as const) if (typeof body[key] === 'string') next[key] = body[key]
        if (typeof body.retire === 'boolean') next.status = body.retire ? 'retired' : 'active'
        if (typeof body.note === 'string' && body.note) next.notes = [...(next.notes || []), { ts: '2026-10-02T09:00:00Z', actor: body.updatedBy || 'agent:archon', text: body.note }]
        next.rev++
        cards[card.id] = next
        return respond(read(next), `${next.id}-${next.rev}`)
      }
      return respond(read(card), `${card.id}-${card.rev}`)
    }
    return fail(404, 'NOT_FOUND', `Fixture has no ${path}`)
  })
  return { patches, boardPatches, cards, deletions, missions }
}
