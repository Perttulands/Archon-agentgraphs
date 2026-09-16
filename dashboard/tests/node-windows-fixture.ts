import { type Page } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

/* The Wayfinding fixture with long authored text in place of its briefs, goal
 * and criteria, and persona cards for its staffing, so a spec can tell whether
 * every word of a node can be read in its window. */

type Node = { id: string; title: string }

/** Several paragraphs and a list, longer than any card shows, ending in a sentence a spec can look for. */
export function authoredText(title: string, field: string): string {
  return [
    `${title}: read the operator's sketch and every source it names before you begin.`,
    '',
    ...Array.from({ length: 6 }, (_, i) => `${i + 1}. Point ${i + 1} of the ${field}, written long enough to wrap across several lines on a card and to be cut short there.`),
    '',
    `The ${field} for ${title} ends here.`,
  ].join('\n')
}

export const authoredBoard = {
  ...wayfinding.board,
  missions: wayfinding.board.missions.map((mission: Node) => ({ ...mission, goal: authoredText(mission.title, 'mission goal'), beadId: 'form-3yd.10' })),
  formations: wayfinding.board.formations.map((formation: Node) => ({ ...formation, brief: { goal: authoredText(formation.title, 'brief') } })),
  gates: wayfinding.board.gates.map((gate: Node) => ({ ...gate, criterion: authoredText(gate.title, 'criterion') })),
}

const agents = ['codex-scout', 'delivery-planner', 'codex-planner', 'codex-judge'].map(id => ({
  id,
  displayName: id.split('-').map(word => word[0].toUpperCase() + word.slice(1)).join(' '),
  harnessDefault: id.startsWith('codex') ? 'openai-codex' : 'claude-code',
  assignable: true,
  liveness: 'offline',
  tags: [],
  kind: 'specialist',
}))

/** Serves the authored Wayfinding board, without notes, and records every write. */
export async function nodeWindowsFixture(page: Page) {
  // Routes added later answer first, so these replace the board, notes and agents of the Wayfinding fixture.
  const fixture = await wayfindingFixture(page)
  const respond = (data: unknown) => ({ json: { success: true, data }, headers: { ETag: 'fixture-etag' } })
  await page.route('**/api/formations/boards/wayfinding', route => route.fulfill(respond({ board: authoredBoard })))
  await page.route('**/api/formations/boards/wayfinding/notes', route => route.fulfill(respond({ notes: { ...wayfinding.notes, board: [], elements: [] } })))
  await page.route('**/api/agents', route => route.fulfill(respond({ agents, count: agents.length })))
  for (const agent of agents) {
    const model = agent.harnessDefault === 'openai-codex' ? 'gpt-5.5' : 'opus'
    await page.route(`**/api/agents/${agent.id}`, route => route.fulfill(respond({
      ...agent, summary: '', harnessVariants: [{ id: agent.harnessDefault, model, effort: 'medium' }], etag: `${agent.id}-card`,
    })))
  }
  return fixture
}
