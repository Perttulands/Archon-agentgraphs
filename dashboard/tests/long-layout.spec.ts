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
