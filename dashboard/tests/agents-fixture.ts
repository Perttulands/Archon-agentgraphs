import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))

// Three missions for the shared current mission and a judged mission, and two
// personas whose settings the daemon validates and whose seat launch it
// renders, as src/internal/formations/harness_launch.go does.

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
    inputCards: [mission('Scratch work')], formations: [solo('work', 'Work', 'builder', 'openai-codex')], gates: [],
    connections: [{ id: 'a1', from: 'mission:out', to: 'work:in' }],
  },
  delivery: {
    id: 'brd_delivery', slug: 'delivery', title: 'Delivery', rev: 4, etag: 'delivery-4',
    inputCards: [mission('Ship the brief')],
    formations: [solo('build', 'Build', 'builder', 'openai-codex'), solo('judge', 'Beads reviewer', 'critic'), solo('recheck', 'Second opinion'), solo('ship', 'Ship', 'builder', 'openai-codex')],
    gates: [{ id: 'review', title: 'Beads review', kinds: ['formation'], criterion: 'The Beads pass lint.' }],
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
    inputCards: [mission('Map the terrain')], formations: [solo('map', 'Map', 'critic')], gates: [],
    connections: [{ id: 'w1', from: 'mission:out', to: 'map:in' }],
  },
}
const layouts: Record<string, Array<{ id: string; x: number; y: number }>> = {
  alpha: [{ id: 'mission', x: 100, y: 100 }, { id: 'work', x: 420, y: 100 }],
  delivery: [{ id: 'mission', x: 100, y: 100 }, { id: 'build', x: 420, y: 100 }, { id: 'review', x: 760, y: 100 },
    { id: 'judge', x: 760, y: 420 }, { id: 'recheck', x: 1080, y: 420 }, { id: 'ship', x: 1100, y: 100 }],
  scouting: [{ id: 'mission', x: 100, y: 100 }, { id: 'map', x: 420, y: 100 }],
}

export const harnesses = [
  { id: 'claude-code', executable: 'claude', efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium' },
  { id: 'openai-codex', executable: 'codex', efforts: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'], defaultEffort: 'medium' },
]

/** The effort policy the daemon serves with the roster (slot_settings.go). */
export const effortPolicy = [
  { effort: 'low', use: 'errands' },
  { effort: 'medium', use: 'making things' },
  { effort: 'xhigh', use: 'architecture and review' },
  { effort: 'max', use: 'consequential reviews' },
]

type Variant = { id: string; sessionStem: string; model?: string; effort?: string }
type Card = { id: string; displayName: string; kind: string; summary: string; tags: string[]; harnessDefault: string; harnessVariants: Variant[]; rev: number }

const quote = (value: string) => `'${value.replaceAll("'", `'"'"'`)}'`
/** The seat launcher's rendering (HarnessVariant.RenderLaunch) with the CLI on the daemon's PATH. */
function describe(variant: Variant) {
  const harness = harnesses.find(next => next.id === variant.id)
  if (!harness) return { ...variant, seatLaunchError: `unsupported seat harness "${variant.id}"` }
  const effort = variant.effort || 'medium'
  let seatLaunch = `exec ${quote(`/usr/local/bin/${harness.executable}`)}${variant.model ? ` --model ${quote(variant.model)}` : ''}`
  seatLaunch += variant.id === 'openai-codex'
    ? ` -c ${quote(`model_reasoning_effort="${effort}"`)} -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox`
    : ` --effort ${quote(effort)} --dangerously-skip-permissions`
  return { ...variant, effectiveEffort: effort, efforts: harness.efforts, seatLaunch }
}

export async function agentsFixture(page: Page) {
  const cards: Record<string, Card> = {
    critic: { id: 'critic', displayName: 'Brief critic', kind: 'judge', summary: 'Reviews the brief.', tags: ['review'], harnessDefault: 'claude-code', rev: 1,
      harnessVariants: [{ id: 'claude-code', sessionStem: 'critic', model: 'claude-opus-5', effort: 'low' }] },
    builder: { id: 'builder', displayName: 'Builder', kind: 'builder', summary: 'Builds the change.', tags: ['implement'], harnessDefault: 'openai-codex', rev: 1,
      harnessVariants: [{ id: 'openai-codex', sessionStem: 'builder' }, { id: 'claude-code', sessionStem: 'claude-builder' }] },
    spawner: { id: 'spawner', displayName: 'Hermes spawner', kind: 'specialist', summary: 'Runs through hermes.', tags: [], harnessDefault: 'hermes', rev: 1,
      harnessVariants: [{ id: 'hermes', sessionStem: 'spawner' }] },
  }
  const patches: unknown[] = []
  const read = (card: Card) => ({ ...card, etag: `${card.id}-${card.rev}`, harnessVariants: card.harnessVariants.map(describe) })

  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    const respond = (data: unknown, etag = 'fixture-etag', status = 200) => route.fulfill({ status, json: { success: true, data }, headers: { ETag: etag } })
    const fail = (status: number, code: string, message: string) => route.fulfill({ status, json: { success: false, error: { code, message } } })
    if (path === '/api/theme') return route.fulfill({ json: defaultTheme })
    if (path === '/api/missions') return respond({ missions: Object.values(boards) })
    const boardMatch = path.match(/^\/api\/missions\/([^/]+)(\/.*)?$/)
    if (boardMatch) {
      const board = boards[boardMatch[1] as keyof typeof boards]
      if (!board) return fail(404, 'NOT_FOUND', 'Mission not found')
      const rest = boardMatch[2] || ''
      if (rest === '/layout') return respond({ layout: { missionId: board.id, missionRev: board.rev, etag: `${board.slug}-layout`, nodes: layouts[board.slug], edges: [] } })
      if (rest === '/notes') return respond({ notes: { schema: 2, missionId: board.id, rev: 1, mission: [], elements: [], updatedAt: '2026-09-29T00:00:00Z', etag: 'notes-1' } })
      if (rest === '/changes') return respond({ signal: { changed: false } })
      if (rest === '/validation') return respond({ missionRev: board.rev, missionEtag: board.etag, errors: [], warnings: [] })
      if (rest === '') return respond({ mission: board }, board.etag)
    }
    if (path === '/api/runs') return respond([])
    if (path === '/api/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/agents') {
      const agents = Object.values(cards).map(card => ({ id: card.id, displayName: card.displayName, kind: card.kind, tags: card.tags, harnessDefault: card.harnessDefault, liveness: 'offline', assignable: true }))
      return respond({ agents, count: agents.length, harnesses, effortPolicy })
    }
    const agentMatch = path.match(/^\/api\/agents\/([^/]+)$/)
    if (agentMatch) {
      const card = cards[agentMatch[1]]
      if (!card) return fail(404, 'NOT_FOUND', 'Agent not found')
      if (method === 'PATCH') {
        if (route.request().headers()['if-match'] !== `${card.id}-${card.rev}`) return fail(409, 'CONFLICT', 'Agent card changed; reload and retry')
        const body = route.request().postDataJSON()
        patches.push(body)
        const next = structuredClone(card)
        const settings = [...(body.variants || []), ...(body.model !== undefined || body.effort !== undefined ? [{ id: body.variant || '', model: body.model, effort: body.effort }] : [])]
        const named = new Set<string>()
        for (const setting of settings) {
          const variant = next.harnessVariants.find(candidate => candidate.id === (setting.id || next.harnessDefault))
          if (!variant) return fail(422, 'INVALID_AGENT_CARD', `agent "${card.id}" has no harness variant "${setting.id}"`)
          if (named.has(variant.id)) return fail(422, 'INVALID_AGENT_CARD', `agent "${card.id}" harness variant "${variant.id}" is edited twice in one change; name each variant once`)
          named.add(variant.id)
          const harness = harnesses.find(candidate => candidate.id === variant.id)
          // Only the fields being changed are validated.
          if ((setting.model?.trim() || setting.effort?.trim()) && !harness) return fail(422, 'INVALID_AGENT_CARD', `agent "${card.id}" harness "${variant.id}" has no model or effort setting`)
          const effort = setting.effort?.trim() || ''
          if (harness && effort && !harness.efforts.includes(effort)) return fail(422, 'INVALID_AGENT_CARD', `agent "${card.id}" effort "${effort}" is not one ${harness.id} accepts; use ${harness.efforts.join(', ')}`)
          if (setting.model !== undefined) variant.model = setting.model.trim() || undefined
          if (setting.effort !== undefined) variant.effort = setting.effort.trim() || undefined
        }
        for (const key of ['displayName', 'kind', 'summary'] as const) if (typeof body[key] === 'string') next[key] = body[key]
        if (typeof body.sessionStem === 'string') next.harnessVariants.find(variant => variant.id === next.harnessDefault)!.sessionStem = body.sessionStem
        next.rev++
        cards[card.id] = next
        return respond(read(next), `${next.id}-${next.rev}`)
      }
      return respond(read(card), `${card.id}-${card.rev}`)
    }
    return fail(404, 'NOT_FOUND', `Fixture has no ${path}`)
  })
  return { patches, cards }
}
