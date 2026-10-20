import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

test('51-run discovery shares one compact index and selected history has one serial stream', async ({ page }) => {
  await page.clock.install()
  await cockpitFixture(page)
  const history = Array.from({ length: 63 }, (_, i) => ({ runId: 'run_live', seq: i + 1, type: i ? 'node_started' : 'run_started', nodeId: 'execution' }))
  const summary = (i: number) => ({ runId: `run_${i}`, missionSlug: 'browser', missionId: 'brd_browser', missionRev: 1,
    status: 'succeeded', final: true, inputCardId: 'mission', eventCount: 63, inputExcerpt: 'Verify the result', sessions: [] })
  const index = Array.from({ length: 51 }, (_, i) => summary(i))
  let discovered = false
  let listCalls = 0, detailCalls = 0, eventsCalls = 0, inFlight = 0, maxInFlight = 0
  const queries: string[] = []
  let release!: () => void
  const held = new Promise<void>(resolve => { release = resolve })
  await page.route('**/api/runs**', async route => {
    const url = new URL(route.request().url())
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
    if (url.pathname === '/api/runs') {
      listCalls++
      queries.push(url.search)
      return respond(discovered ? [...index, { ...summary(52), runId: 'run_external', status: 'running', final: false }] : index)
    }
    if (url.pathname.endsWith('/events')) eventsCalls++
    if (url.pathname === '/api/runs/run_live') {
      detailCalls++
      maxInFlight = Math.max(maxInFlight, ++inFlight)
      await held
      inFlight--
      return respond({ ...summary(0), runId: 'run_live', status: 'running', final: false, events: history })
    }
    return route.fallback()
  })
  await page.goto('/?mission=browser&run=run_live')
  await expect.poll(() => detailCalls).toBe(1)
  await expect.poll(() => listCalls).toBe(1)
  await page.clock.runFor(5200)
  await expect.poll(() => listCalls).toBe(2)
  expect(detailCalls).toBe(1)
  expect(maxInFlight).toBe(1)
  release()
  await expect(page.getByTestId('run-banner')).toContainText('Running')
  discovered = true
  await page.clock.runFor(5200)
  await expect(page.getByTestId('nav-run-run_external')).toBeVisible()
  expect(queries.every(query => query === '')).toBe(true)
  expect(eventsCalls).toBe(0)
  expect(maxInFlight).toBe(1)
  const compactBytes = Buffer.byteLength(JSON.stringify(index))
  const oldBytes = Buffer.byteLength(JSON.stringify(index.map(run => ({ ...run, events: history }))))
  expect(compactBytes).toBeLessThan(oldBytes / 5)
  test.info().annotations.push({ type: 'network', description: JSON.stringify({ runs: 51, compactBytes, oldBytes, listCalls, detailCalls, eventsCalls, maxInFlight }) })
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const stopped = detailCalls
  await page.clock.runFor(6000)
  expect(detailCalls).toBe(stopped)
})
