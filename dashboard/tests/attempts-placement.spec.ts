import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

const overlaps = (a: { x: number; y: number; width: number; height: number }, b: { x: number; y: number; width: number; height: number }) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height

test('a card reserves its produced row before evidence arrives, keeping an opened attempts window clear', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page, { succeeded: true })
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route('**/api/runs/run_browser/evidence/artifacts', async route => {
    await pending
    await route.fallback()
  })
  await page.goto('/?mission=browser&run=run_browser')
  const card = page.locator('.formation[data-node="execution"]')
  const produced = card.getByTestId('produced-execution')
  await expect(produced).toBeAttached()
  await expect(produced.locator('button')).toHaveCount(0)
  const before = (await card.boundingBox())!
  await card.getByTestId('attempts-open-execution').click()
  const attempts = page.getByRole('dialog', { name: 'Attempts of Execution' })
  await expect(attempts.getByTestId('attempt-1')).toBeVisible()
  const opened = (await attempts.boundingBox())!
  release()
  await expect(produced.locator('button').first()).toBeVisible()
  expect(await card.boundingBox()).toEqual(before)
  expect(await attempts.boundingBox()).toEqual(opened)
  expect(overlaps(opened, before)).toBe(false)
})

for (const [width, height] of [[1920, 1080], [2560, 1440]]) {
  for (const view of ['Canvas', 'Flow'] as const) {
    test(`attempts leave their opening ${view === 'Canvas' ? 'card' : 'Flow status'} clear at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height })
      await page.addInitScript(() => localStorage.clear())
      await cockpitFixture(page, { succeeded: true, failedGate: true })
      await page.goto('/?mission=browser&run=run_browser')
      await page.getByRole('radio', { name: view, exact: true }).click()
      const source = page.locator(view === 'Canvas' ? '.formation[data-node="execution"]' : '.flow-step[data-flow-node="execution"] .flow-status')
      await source.getByTestId('attempts-open-execution').click()
      const attempts = page.getByRole('dialog', { name: 'Attempts of Execution' })
      await expect(attempts.getByTestId('attempt-1')).toBeVisible()
      const opened = (await attempts.boundingBox())!
      expect(overlaps(opened, (await source.boundingBox())!)).toBe(false)
      const gate = page.locator(view === 'Canvas' ? '.gatecard[data-node="gate"]' : '.flow-step[data-flow-node="gate"]')
      const protectedParts = view === 'Canvas' ? gate : gate.locator('.flow-status, .flow-verdict')
      await expect(gate).toContainText(/fail/i)
      if (view === 'Flow') await expect(gate.locator('.flow-verdict')).toBeVisible()
      const sourceBox = (await source.boundingBox())!
      const gateBoxes = await Promise.all((await protectedParts.all()).map(async part => (await part.boundingBox())!))
      // The reading floor replaces the old rule that shrank to clear every gate.
      // Check whether any floor-sized rectangle could leave source and gate clear.
      const obstacles = [sourceBox, ...gateBoxes]
      const xs = [268, ...obstacles.flatMap(box => [box.x - 560, box.x + box.width])]
      const floorHeight = Math.ceil(height * .7)
      const ys = [85, ...obstacles.flatMap(box => [box.y - floorHeight, box.y + box.height])]
      const gateFits = xs.some(x => ys.some(y => x >= 268 && x + 560 <= width - 8 && y >= 85 && y + floorHeight <= height - 8
        && obstacles.every(box => !overlaps({ x, y, width: 560, height: floorHeight }, box))))
      if (gateFits) for (const box of gateBoxes) expect(overlaps(opened, box)).toBe(false)
      expect(opened.width).toBeGreaterThanOrEqual(560)
      expect(opened.height).toBeGreaterThanOrEqual(Math.ceil(height * .7))
      expect(opened.x).toBeGreaterThanOrEqual(0)
      expect(opened.y).toBeGreaterThanOrEqual(0)
      expect(opened.x + opened.width).toBeLessThanOrEqual(width)
      expect(opened.y + opened.height).toBeLessThanOrEqual(height)
    })
  }
}
