import { expect, test } from '@playwright/test'
import { cockpitFixture, reviewMarkdown } from './cockpit-fixture'
import { arrangedLayoutNodes } from '../src/components/formationsArrange'

/* Runs are listed under their mission and open onto what they produced
 * (archon-o7p.2). The routes answer as archond does: the run list holds the
 * mission's own runs with their driver, times and input excerpts, an artifact preview
 * names its absolute path, and the mission as a run ran it is evidence. */

const artifactRoot = '/srv/scratch/archon-state/.archon/artifacts/run_browser'
const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()
// The shared discovery index carries excerpts and sessions, never event histories.
const projection = (runId: string, status: string, final: boolean, extra: Record<string, unknown>) => ({
  runId, status, final, missionSlug: 'browser', missionId: 'brd_browser', missionRev: 1, inputCardId: 'mission', eventCount: 7,
  epoch: 0, resumeAllowed: false, humanChannel: 'notify', inputExcerpt: '', sessions: [], ...extra,
})
const runs = [
  projection('run_browser', 'succeeded', true, { startedAt: ago(90), updatedAt: ago(86), startedBy: 'agent:driver',
    inputExcerpt: 'Verify the retry budget and log the second failure' }),
  projection('run_01OLDER', 'failed', true, { startedAt: ago(300), updatedAt: ago(299), startedBy: 'human:ui', missionRev: 1,
    inputExcerpt: 'First attempt at the review' }),
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
      if (url.pathname === '/api/runs') return respond(missionRuns)
      if (url.pathname === '/api/runs/run_browser') return respond({ ...missionRuns[0], events: fixture.events() })
      if (url.pathname === '/api/runs/run_browser/evidence/artifacts/review.md') return respond({ artifact: { name: 'review.md', size: reviewMarkdown.length,
        modifiedAt: '2026-09-16T00:00:00Z', kind: 'markdown', path: `${artifactRoot}/review.md`, text: { text: reviewMarkdown, bytes: reviewMarkdown.length } } })
      if (url.pathname === '/api/runs/run_browser/evidence/mission') return respond({ mission: { missionRev: 2, graph: { ...fixture.board(), rev: 2, formations: fixture.board().formations.map(node => ({...node, title: 'Frozen '+node.title})) }, text: { text: 'schema = 1\nslug = "browser"\nrev = 2\n', bytes: 36 } } })
      return route.fallback()
    })
    await page.goto(entry === 'list' ? '/?mission=browser' : '/?mission=browser&run=run_browser')

    if (entry === 'list') {
      // No run is open: the navigator lists the mission's finished runs, each named without its ID.
      const rows = page.getByTestId('nav-group-browser').locator('[data-testid^="nav-run-"]')
      await expect(rows).toHaveCount(2)
      await expect(rows.nth(0)).toContainText('succeeded')
      await expect(rows.nth(0)).toContainText('Verify the retry budget and log the second failure')
      await expect(rows.nth(0)).not.toContainText('run_browser')
      await expect(rows.nth(1)).toContainText('failed')
      await expect(rows.nth(1)).toContainText('First attempt at the review')
      await page.screenshot({ path: test.info().outputPath('run-list.png') })
      await rows.nth(0).click()
    }
    const banner = page.getByTestId('run-banner')
    await expect(banner.locator('.badge')).toHaveText('Succeeded')
    await expect(banner.locator('.run-when')).toContainText('took 4m · agent:driver')
    await expect(page).toHaveURL(/\?mission=browser&run=run_browser$/)

    // The board has moved on: both views use the frozen graph and disable edits.
    await expect(page.getByTestId('canvas-dock')).toContainText('Showing revision 2 · read only')
    await expect(page.getByTestId('new-formation')).toHaveCount(0)
    await expect(page.getByTitle('Start mission', { exact: true }).first()).toBeDisabled()
    await expect(page.getByTitle('Run formation', { exact: true }).first()).toBeDisabled()
    await expect(page.locator('.formation').first()).toContainText('Frozen')
    // Frozen geometry comes from its own graph, never the current saved layout.
    const first = page.locator('.formation').first()
    const saved = await page.evaluate(async id => (await (await fetch('/api/missions/browser/layout')).json()).data.layout.nodes.find((node: { id: string }) => node.id === id), await first.getAttribute('data-node'))
    const arranged = arrangedLayoutNodes(fixture.board()).find(node => node.id === saved.id)!
    await expect(first).toHaveCSS('left', `${arranged.x}px`)
    await expect(first).toHaveCSS('top', `${arranged.y}px`)
    // A frozen card does not move, and a click on it still opens its window, read only.
    await first.locator('.tt').click()
    const frozenStep = page.getByRole('dialog', { name: /^Formation · / })
    await expect(frozenStep).toBeVisible()
    await expect(frozenStep.getByRole('button', { name: /^Change the role of / }).first()).toBeDisabled()
    await frozenStep.getByRole('button', { name: /^Close Formation · / }).click()
    await expect(frozenStep).toHaveCount(0)
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

    // An output opens its attempt; its file offers Copy path for the snapshot.
    await banner.getByRole('button', { name: 'review.md' }).click()
    const attempts = page.getByRole('dialog', { name: 'Attempts of Peer review' })
    await expect(attempts.getByRole('heading', { name: 'Peer review' })).toBeVisible()
    await attempts.getByRole('button', { name: 'review.md', exact: true }).click()
    const review = page.getByRole('dialog', { name: /review\.md/ })
    await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()
    await review.getByRole('button', { name: 'Copy path' }).click()
    await expect(review.getByRole('button', { name: 'Copied' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${artifactRoot}/review.md`)
    expect(fixture.writes).toEqual([])
    // Flow has no canvas palette; its mission remains editable from the navigator.
    await page.getByRole('button', { name: /^Edit mission/ }).click()
    await expect(page.getByTestId('flow-view')).not.toContainText('Frozen')
    await expect(page.getByTestId('canvas-dock')).toHaveCount(0)
    await page.getByRole('radio', { name: 'Canvas', exact: true }).click()
    await expect(page.getByTestId('new-formation')).toBeEnabled()
    await expect(first).toHaveCSS('left', `${saved.x}px`)
    await expect(first).toHaveCSS('top', `${saved.y}px`)
    expect(fixture.writes).toEqual([])
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
    if (path === '/api/runs/run_browser') return route.fulfill({ json: { success: true, data: { ...runs[0], missionRev: 2, events: fixture.events() } } })
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
  await page.route('**/api/runs', route => route.fulfill({ json: { success: true, data: [waiting] } }))
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('nav-group-needs-you').locator('.nav-row-sub')).toContainText(['Peer and judge'])
  await expect(page).toHaveTitle('(1) Peer and judge · Archon')
  const badge = page.getByTestId('run-banner').locator('.badge')
  await expect(badge).toHaveText('Waiting for your answer')
  const [badgeColor, badgeBackground] = await badge.evaluate(element => [getComputedStyle(element).color, getComputedStyle(element).backgroundColor])
  expect(badgeBackground).not.toBe('rgba(0, 0, 0, 0)')
  expect(badgeColor).not.toBe(badgeBackground)
  await expect(page.getByTestId('run-chip-loose')).toHaveText('waiting for you')
  await page.screenshot({ path: test.info().outputPath('waiting.png') })
})


test('runs of a TOML mission keep their start, status and Bead visible when the inputs repeat', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const fixture = await cockpitFixture(page, { tomlMission: true })
  const subject = 'The same character request '.repeat(12)
  const recent = projection('run_01LATEST', 'succeeded', true, { startedAt: ago(10), updatedAt: ago(8), startedBy: 'agent:identity', beadId: 'archon-identity',
    inputExcerpt: `${subject.slice(0, 89)}…` })
  const older = projection('run_01BEFORE', 'failed', true, { ...recent, runId: 'run_01BEFORE', status: 'failed', startedAt: ago(20), updatedAt: ago(18) })
  await page.route('**/api/runs**', route => {
    const path = new URL(route.request().url()).pathname
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
    if (path === '/api/runs') return respond([recent, older])
    if (path === '/api/runs/run_01LATEST') return respond({ ...recent, events: fixture.events().map(event => ({ ...event, runId: recent.runId })) })
    if (path === '/api/runs/run_01LATEST/evidence/mission') return respond({ mission: { missionRev: 1, graph: fixture.board(), text: { text: '', bytes: 0 } } })
    return route.fallback()
  })
  await page.goto('/?mission=browser')
  const rows = page.getByTestId('nav-group-browser').locator('[data-testid^="nav-run-"]')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0).locator('.nav-row-name')).toHaveText(/\d{2}:\d{2}:\d{2}$/)
  expect(await rows.nth(0).locator('.nav-row-name').textContent()).not.toBe(await rows.nth(1).locator('.nav-row-name').textContent())
  await expect(rows.nth(0).locator('.nav-state')).toHaveText('succeeded')
  await expect(rows.nth(1).locator('.nav-state')).toHaveText('failed')
  await expect(rows.nth(0).locator('.nav-row-sub')).toHaveText('archon-identity')
  await expect(rows.nth(0)).toHaveAttribute('title', new RegExp('archon-identity.*The same character request'))
  await expect(page.getByTestId('slot-judge-judge_1')).toContainText('claude-opus-5-5')
  await expect(page.getByTestId('slot-judge-judge_1').locator('.slot-warn')).toHaveCount(0)
  await expect(page.getByTestId('staffing-keyhint')).toHaveCount(0)
  await rows.nth(0).click()
  await expect(page.getByTestId('run-banner')).toContainText('agent:identity')
  await expect(page.getByTestId('staffing-keyhint')).toHaveCount(0)
})

for (const view of ['Canvas', 'Flow'] as const) {
  test(`selecting and clearing a run keeps ${view} in place, including while loading`, async ({ page }) => {
    await cockpitFixture(page, { succeeded: true })
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    await page.route('**/api/runs**', async route => {
      const url = new URL(route.request().url())
      if (url.pathname === '/api/runs')
        return route.fulfill({ json: { success: true, data: [runs[0]] } })
      if (url.pathname === '/api/runs/run_browser') await pending
      return route.fallback()
    })
    await page.goto('/?mission=browser')
    await expect(page.getByTestId('nav-run-run_browser')).toBeVisible()
    await page.getByRole('radio', { name: view, exact: true }).click()
    const viewport = page.getByTestId('formations-canvas')
    const target = view === 'Canvas' ? page.locator('.formation[data-node]').first() : viewport
    await expect(target).toBeVisible()
    const position = async () => {
      const box = await target.boundingBox()
      // Run output can grow a card; its position and zoom must stay put.
      return box && { x: box.x, y: box.y, width: box.width }
    }
    const before = await position()
    await page.getByTestId('nav-run-run_browser').click()
    await expect.poll(position).toEqual(before)
    release()
    await expect(page.getByTestId('run-banner')).toBeVisible()
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await expect.poll(position).toEqual(before)
    await page.getByRole('button', { name: 'Edit mission', exact: false }).first().click()
    await expect(page.getByTestId('run-banner')).toHaveCount(0)
    await expect.poll(position).toEqual(before)
  })
}
