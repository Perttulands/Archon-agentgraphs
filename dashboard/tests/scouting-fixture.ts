import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { rosterAnswer } from './roster-terms'

/* The Scouting mission as the daemon serves it (tests/fixtures/scouting.json):
 * the arrange testdata's structure and Arrange layout, trimmed briefs and
 * criteria, and note threads where several nodes carry both operator and agent
 * entries. Reads only; any write is recorded and refused. */

const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))
export const scouting = JSON.parse(readFileSync(new URL('./fixtures/scouting.json', import.meta.url), 'utf8'))

/** `mission` replaces the served mission, for a test that needs a variation of it. */
export async function scoutingFixture(page: Page, options: { mission?: typeof scouting.mission } = {}) {
  const board = options.mission || scouting.mission
  const writes: string[] = []
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    const respond = (data: unknown, etag = 'fixture-etag') => route.fulfill({ json: { success: true, data }, headers: { ETag: etag } })
    if (method !== 'GET') {
      writes.push(`${method} ${path}`)
      return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'The Scouting fixture is read-only' } } })
    }
    if (path === '/api/theme') return route.fulfill({ json: defaultTheme })
    if (path === '/api/missions') return respond({ missions: [{ id: board.id, slug: 'scouting', title: 'Scouting', rev: board.rev, etag: board.etag }] })
    if (path === '/api/missions/scouting') return respond({ mission: board }, board.etag)
    if (path === '/api/missions/scouting/layout') return respond({ layout: scouting.layout }, scouting.layout.etag)
    if (path === '/api/missions/scouting/notes') return respond({ notes: scouting.notes }, scouting.notes.etag)
    if (path === '/api/missions/scouting/changes') return respond({ signal: { changed: false } })
    if (path === '/api/missions/scouting/validation') return respond({ missionRev: board.rev, missionEtag: board.etag, errors: [], warnings: [] })
    if (path === '/api/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/runs') return respond([])
    if (path === '/api/agents') return respond(rosterAnswer([]))
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  return { writes }
}
