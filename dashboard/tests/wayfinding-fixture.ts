import { type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

/* The Wayfinding board as the daemon serves it (tests/fixtures/wayfinding.json):
 * the arrange testdata's structure and Arrange layout, trimmed briefs and
 * criteria, and note threads where several nodes carry both operator and agent
 * entries. Reads only; any write is recorded and refused. */

const defaultTheme = JSON.parse(readFileSync(new URL('../../src/internal/api/theme_default.json', import.meta.url), 'utf8'))
export const wayfinding = JSON.parse(readFileSync(new URL('./fixtures/wayfinding.json', import.meta.url), 'utf8'))

export async function wayfindingFixture(page: Page) {
  const writes: string[] = []
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = route.request().method()
    const respond = (data: unknown, etag = 'fixture-etag') => route.fulfill({ json: { success: true, data }, headers: { ETag: etag } })
    if (method !== 'GET') {
      writes.push(`${method} ${path}`)
      return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'The Wayfinding fixture is read-only' } } })
    }
    if (path === '/api/theme') return route.fulfill({ json: defaultTheme })
    if (path === '/api/formations/boards') return respond({ boards: [{ id: wayfinding.board.id, slug: 'wayfinding', title: 'Wayfinding', rev: wayfinding.board.rev, etag: wayfinding.board.etag }] })
    if (path === '/api/formations/boards/wayfinding') return respond({ board: wayfinding.board }, wayfinding.board.etag)
    if (path === '/api/formations/boards/wayfinding/layout') return respond({ layout: wayfinding.layout }, wayfinding.layout.etag)
    if (path === '/api/formations/boards/wayfinding/notes') return respond({ notes: wayfinding.notes }, wayfinding.notes.etag)
    if (path === '/api/formations/boards/wayfinding/changes') return respond({ signal: { changed: false } })
    if (path === '/api/formations/boards/wayfinding/validation') return respond({ boardRev: wayfinding.board.rev, boardEtag: wayfinding.board.etag, errors: [], warnings: [] })
    if (path === '/api/formations/gate-profiles') return respond({ profiles: [] })
    if (path === '/api/formations/runs') return respond([])
    if (path === '/api/agents') return respond({ agents: [], count: 0 })
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  return { writes }
}
