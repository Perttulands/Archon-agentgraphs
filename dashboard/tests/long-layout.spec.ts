import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'
import golden from '../src/components/testdata/arrange-long-golden.json' with { type: 'json' }
import { transitionsSettled } from './settled'

for (const width of [1920, 2560]) test(`long graph remains readable and Arrange is deliberate at ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: 1080 })
  const fixture = await cockpitFixture(page)
  Object.assign(fixture.board(), structuredClone(golden.board), { slug: 'browser', etag: 'long-board' })
  let positions = structuredClone(golden.previousPositions)
  let revision = 1
  const writes: unknown[] = []
  await page.route('**/api/missions/browser/layout', route => {
    if (route.request().method() === 'PATCH') {
      const patch = route.request().postDataJSON()
      writes.push(patch)
      positions = patch.arrange ? structuredClone(golden.positions) : patch.nodes
      revision++
    }
    return route.fulfill({ json: { success: true, data: { layout: { missionId: golden.board.id, missionRev: 87, etag: `layout-${revision}`, nodes: positions, edges: [] } } } })
  })
  await page.goto('/?mission=browser')
  await expect(page.locator('.world [data-node]')).toHaveCount(golden.positions.length)
  await transitionsSettled(page)
  expect(positions).toEqual(golden.previousPositions)
  expect(writes).toEqual([])
  await page.screenshot({ path: test.info().outputPath('before-arrange.png') })
  await page.getByTestId('arrange-layout').click()
  await expect.poll(() => writes.length).toBe(1)
  await transitionsSettled(page)
  expect(positions).toEqual(golden.positions)
  const geometry = await page.locator('.world [data-node]').evaluateAll(cards => cards.map(card => {
    const rect = card.getBoundingClientRect()
    const scale = rect.width / (card as HTMLElement).offsetWidth
    const texts = [...card.querySelectorAll('.tt,.mtitle,.gt,.slot-label,.slot-caption')]
      .filter(el => el.textContent?.trim())
      .map(el => parseFloat(getComputedStyle(el).fontSize) * scale)
    return { id: card.getAttribute('data-node'), x: parseFloat((card as HTMLElement).style.left), right: parseFloat((card as HTMLElement).style.left) + (card as HTMLElement).offsetWidth, texts }
  }))
  expect(Math.max(...geometry.map(card => card.right)) - Math.min(...geometry.map(card => card.x))).toBeLessThan(1800)
  for (const card of geometry) for (const size of card.texts) expect(size, `${card.id} text size`).toBeGreaterThanOrEqual(10.99)
  await page.screenshot({ path: test.info().outputPath('reading-start.png') })
  // Explicit overview remains available, without changing the authored layout.
  await page.getByTitle('Fit', { exact: true }).click()
  await transitionsSettled(page)
  await page.screenshot({ path: test.info().outputPath('whole-graph.png') })
  expect(writes).toHaveLength(1)
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect.poll(() => positions).toEqual(golden.previousPositions)
})

test('two frozen revisions use their own nodes and leave current positions untouched', async ({ page }) => {
  const fixture = await cockpitFixture(page)
  Object.assign(fixture.board(), structuredClone(golden.board), { slug: 'browser', etag: 'long-board' })
  const frozen = [44, 43].map((rev, i) => {
    const graph = { ...structuredClone(golden.board), slug: 'browser', rev, etag: `frozen-${rev}` }
    const removed = new Set(graph.formations.slice(-i - 1).map(node => node.id))
    graph.formations = graph.formations.filter(node => !removed.has(node.id))
    graph.connections = graph.connections.filter(edge => !removed.has(edge.from.split(':')[0]) && !removed.has(edge.to.split(':')[0]))
    return graph
  })
  const summaries = frozen.map(graph => ({ runId: `run_${graph.rev}`, status: 'succeeded', final: true, missionSlug: 'browser', missionId: graph.id, missionRev: graph.rev,
    inputCardId: graph.inputCards[0].id, eventCount: 0, inputExcerpt: `Revision ${graph.rev}`, sessions: [] }))
  let writes = 0
  await page.route('**/api/missions/browser/layout', route => {
    if (route.request().method() !== 'GET') writes++
    return route.fulfill({ json: { success: true, data: { layout: { missionId: golden.board.id, missionRev: 87, etag: 'saved-layout', nodes: golden.previousPositions, edges: [] } } } })
  })
  await page.route('**/api/runs**', route => {
    const path = new URL(route.request().url()).pathname
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
    if (path === '/api/runs') return respond(summaries)
    const i = summaries.findIndex(run => path.startsWith(`/api/runs/${run.runId}`))
    if (i < 0) return route.fallback()
    if (path.endsWith('/evidence/mission')) return respond({ mission: { graph: frozen[i], missionRev: frozen[i].rev, text: { text: '', bytes: 0 } } })
    if (path === `/api/runs/${summaries[i].runId}`) return respond({ ...summaries[i], events: [] })
    return route.fallback()
  })
  await page.goto('/?mission=browser&run=run_44')
  const positions = () => page.locator('.world [data-node]').evaluateAll(cards => cards.map(card => ({ id: card.getAttribute('data-node'), x: (card as HTMLElement).style.left, y: (card as HTMLElement).style.top })))
  await expect(page.getByTestId('canvas-dock')).toContainText('Showing revision 44')
  await transitionsSettled(page)
  const first = await positions()
  await page.getByTestId('nav-run-run_43').click()
  await expect(page.getByTestId('canvas-dock')).toContainText('Showing revision 43')
  await transitionsSettled(page)
  const second = await positions()
  expect(second.length).toBe(first.length - 1)
  await page.getByTestId('nav-run-run_44').click()
  await expect(page.getByTestId('canvas-dock')).toContainText('Showing revision 44')
  await transitionsSettled(page)
  expect(await positions()).toEqual(first)
  await page.getByTestId('nav-mission-browser').click()
  await expect(page.getByTestId('new-formation')).toBeEnabled()
  await transitionsSettled(page)
  expect(await positions()).toEqual(expect.arrayContaining(golden.previousPositions.map(node => ({ id: node.id, x: `${node.x}px`, y: `${node.y}px` }))))
  expect(writes).toBe(0)
  expect(fixture.writes).toEqual([])
})
