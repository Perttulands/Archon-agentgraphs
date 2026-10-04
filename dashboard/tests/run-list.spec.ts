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
  epoch: 0, resumeAllowed: false, humanChannel: 'notify', waitingGates: [], onCallSeats: [], events: [], ...extra,
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
      if (url.pathname === '/api/runs/run_browser/evidence/mission') return respond({ mission: { missionRev: 2, graph: { ...fixture.board(), rev: 2, formations: fixture.board().formations.map(node => ({...node, title: 'Frozen '+node.title})) }, text: { text: 'schema = 1\nslug = "browser"\nrev = 2\n', bytes: 36 } } })
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

    // The board has moved on: both views use the frozen graph and disable edits.
    await expect(banner).toContainText('Showing revision 2 · read only')
    await expect(page.getByTestId('new-formation')).toBeDisabled()
    await expect(page.getByTitle('Start mission', { exact: true }).first()).toBeDisabled()
    await expect(page.getByTitle('Run formation', { exact: true }).first()).toBeDisabled()
    await expect(page.locator('.formation').first()).toContainText('Frozen')
    // Notes opened through the toolbar also respect the historical boundary.
    await page.getByRole('button', { name: 'Mission notes', exact: true }).click()
    const notes = page.getByRole('dialog', { name: 'mission notes', exact: true })
    await expect(notes.getByRole('textbox')).toHaveAttribute('readonly', '')
    await expect(notes.locator('.note-reply-actions button').last()).toBeDisabled()
    for (const control of await notes.locator('.note-entry-actions button').all()) await expect(control).toBeDisabled()
    await notes.getByRole('button', { name: /Close/ }).click()
    await page.getByRole('radio', { name: 'Flow', exact: true }).click()
    await expect(page.getByTestId('flow-view')).toContainText('Frozen')
    await expect(page.getByTestId('flow-view').getByRole('button', { name: 'Start mission', exact: true })).toBeDisabled()
    await page.screenshot({ path: test.info().outputPath('frozen-flow.png') })

    // A produced file is one click from the run, and Copy path gives its absolute path.
    await banner.getByRole('button', { name: 'review.md' }).click()
    const review = page.getByRole('dialog', { name: /review\.md/ })
    await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()
    await review.getByRole('button', { name: 'Copy path' }).click()
    await expect(review.getByRole('button', { name: 'Copied' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${artifactRoot}/review.md`)
    expect(fixture.writes).toEqual([])
    await banner.getByRole('button', { name: /Edit current mission/ }).click()
    await expect(page.getByTestId('new-formation')).toBeEnabled()
    await expect(page.getByTestId('flow-view')).not.toContainText('Frozen')
  })
}

test('an old run link is rejected when its slug now belongs to a recreated mission', async ({ page }) => {
  const fixture = await cockpitFixture(page, { succeeded: true })
  const frozen = structuredClone(fixture.board())
  frozen.rev = 2
  fixture.board().id = 'brd_recreated'
  fixture.board().rev = 3
  await page.route('**/api/runs**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/runs/run_browser') return route.fulfill({ json: { success: true, data: { ...runs[0], missionRev: 2 } } })
    if (path === '/api/runs/run_browser/evidence/mission') return route.fulfill({ json: { success: true, data: { mission: { missionRev: 2, graph: frozen, text: { text: '', bytes: 0 } } } } })
    return route.fallback()
  })
  await page.goto('/?mission=browser&run=run_browser')
  await expect(page.getByTestId('formations-error')).toContainText('belongs to an earlier mission "browser" that was deleted')
  await expect(page.getByTestId('run-banner')).toHaveCount(0)
  await expect(page.getByTestId('new-formation')).toBeEnabled()
})

// Every run that needs the operator is counted on the mission picker and in
// the page title, and waiting is the most visible state (archon-n7u.29).
test('a run waiting for you is counted and stands out on the bar and the canvas', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { waitingHuman: true })
  const waiting = projection('run_browser', 'waiting_human', false, { startedAt: ago(12), updatedAt: ago(10), startedBy: 'agent:driver',
    waitingGates: [{ gateId: 'loose', requestedSeq: 3, requestedAt: ago(10), askedSeats: [] }] })
  await page.route('**/api/runs?needs=you', route => route.fulfill({ json: { success: true, data: [waiting] } }))
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('board-picker').locator('option')).toHaveText(['Peer and judge · 1 needs you'])
  await expect(page).toHaveTitle('(1) Peer and judge · Archon')
  const badge = page.getByTestId('run-banner').locator('.badge')
  await expect(badge).toHaveText('Waiting for your answer')
  const [badgeColor, badgeBackground] = await badge.evaluate(element => [getComputedStyle(element).color, getComputedStyle(element).backgroundColor])
  expect(badgeBackground).not.toBe('rgba(0, 0, 0, 0)')
  expect(badgeColor).not.toBe(badgeBackground)
  await expect(page.getByTestId('run-chip-loose')).toHaveText('waiting for you')
  // The only run that needs you is the one shown, so nothing else is offered.
  await expect(page.getByTestId('run-next-needs-you')).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('waiting.png') })
})
