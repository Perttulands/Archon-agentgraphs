import { expect, test } from '@playwright/test'
import { cockpitFixture, reviewMarkdown } from './cockpit-fixture'

/* Runs are listed under their mission and open onto what they produced
 * (archon-o7p.2). The routes answer as archond does: the run list holds the
 * mission's own runs with their driver, times and inputs, an artifact preview
 * names its absolute path, and the mission as a run ran it is evidence. */

const artifactRoot = '/srv/scratch/archon-state/.archon/artifacts/run_browser'
const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()
const projection = (runId: string, status: string, final: boolean, extra: Record<string, unknown>) => ({
  runId, status, final, missionSlug: 'browser', missionId: 'brd_browser', missionRev: 1, inputCardId: 'mission', eventCount: 7,
  epoch: 0, resumeAllowed: false, projectionVersion: 'standalone-trusted-v1', humanChannel: 'notify', waitingGates: [], onCallSeats: [], events: [], ...extra,
})
const runs = [
  projection('run_browser', 'succeeded', true, { startedAt: ago(90), updatedAt: ago(86), startedBy: 'agent:driver',
    inputs: [{ name: 'brief', kind: 'text', value: 'Verify the retry budget\nand log the second failure' }] }),
  projection('run_01OLDER', 'failed', true, { startedAt: ago(300), updatedAt: ago(299), startedBy: 'human:ui', missionRev: 1,
    inputs: [{ name: 'brief', kind: 'text', value: 'First attempt at the review' }] }),
]

for (const entry of ['list', 'link'] as const) {
  test(`a run ${entry === 'list' ? 'chosen from the mission run list' : 'opened by its link'} reads as it ran and opens what it produced`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.setViewportSize({ width: 1920, height: 1080 })
    const fixture = await cockpitFixture(page, { succeeded: true })
    // The mission has been edited since the runs: it is at revision 3.
    fixture.board().rev = 3
    const missionRuns = runs.map(run => run.runId === 'run_browser' ? { ...run, missionRev: 2 } : run)
    await page.route('**/api/runs**', async route => {
      const url = new URL(route.request().url())
      const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
      if (url.pathname === '/api/runs' && url.searchParams.get('mission') === 'browser') return respond(missionRuns)
      if (url.pathname === '/api/runs/run_browser') return respond(missionRuns[0])
      if (url.pathname === '/api/runs/run_browser/evidence/artifacts/review.md') return respond({ artifact: { name: 'review.md', size: reviewMarkdown.length,
        modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', path: `${artifactRoot}/review.md`, text: { text: reviewMarkdown, bytes: reviewMarkdown.length } } })
      if (url.pathname === '/api/runs/run_browser/evidence/mission') return respond({ mission: { missionRev: 2, text: { text: 'schema = 1\nslug = "browser"\nrev = 2\n', bytes: 36 } } })
      return route.fallback()
    })
    await page.goto(entry === 'list' ? '/?mission=browser' : '/?mission=browser&run=run_browser')

    if (entry === 'list') {
      // No run is open, so the mission's finished runs are one click away, each named without its ID.
      const idle = page.getByTestId('run-banner-idle')
      await idle.getByRole('button', { name: 'Runs (2)' }).click()
      const list = page.getByRole('dialog', { name: 'Runs of Peer and judge' })
      const rows = list.locator('.run-row')
      await expect(rows).toHaveCount(2)
      await expect(rows.nth(0)).toContainText('Succeeded')
      await expect(rows.nth(0)).toContainText('took 4m')
      await expect(rows.nth(0)).toContainText('Verify the retry budget and log the second failure')
      await expect(rows.nth(0)).toContainText('agent:driver')
      await expect(rows.nth(0)).not.toContainText('run_browser')
      await expect(rows.nth(1)).toContainText('Failed')
      await expect(rows.nth(1)).toContainText('First attempt at the review')
      await page.screenshot({ path: test.info().outputPath('run-list.png') })
      await rows.nth(0).click()
    }
    const banner = page.getByTestId('run-banner')
    await expect(banner.locator('.badge')).toHaveText('Succeeded')
    await expect(banner.locator('.run-when')).toContainText('took 4m · agent:driver')
    await expect(page).toHaveURL(/\?mission=browser&run=run_browser$/)

    // The board has moved on since; the run opens the mission as it ran.
    await banner.getByRole('button', { name: 'ran revision 2' }).click()
    const ran = page.getByRole('dialog', { name: /Peer and judge as run .*rowser ran it/ })
    await expect(ran).toContainText('rev = 2')

    // A produced file is one click from the run, and Copy path gives its absolute path.
    await banner.getByRole('button', { name: 'review.md' }).click()
    const review = page.getByRole('dialog', { name: /review\.md/ })
    await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()
    await review.getByRole('button', { name: 'Copy path' }).click()
    await expect(review.getByRole('button', { name: 'Copied' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${artifactRoot}/review.md`)
    expect(fixture.writes).toEqual([])
  })
}
