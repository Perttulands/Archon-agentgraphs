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

for (const [width, height] of [[1536, 864], [1600, 900], [1680, 1050], [1920, 1080], [2560, 1080], [2560, 1440]]) {
  for (const view of ['Canvas', 'Flow'] as const) {
    test(`attempts preserve their reading floor and available source space on ${view} at ${width}x${height}`, async ({ page }) => {
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
      const gateBoxes = await Promise.all((await protectedParts.all()).map(async part => (await part.boundingBox())!))
      // Pin the observed contract per scene, rather than computing whether
      // to assert from another placement algorithm inside this test. The
      // narrower Canvas scenes cannot clear every gate at the reading floor.
      const gateClear = view === 'Flow' || width >= 2560
      expect(gateBoxes.every(box => !overlaps(opened, box))).toBe(gateClear)
      expect(opened.width).toBeGreaterThanOrEqual(560)
      expect(opened.height).toBeGreaterThanOrEqual(Math.ceil(height * .7))
      expect(opened.x).toBeGreaterThanOrEqual(0)
      expect(opened.y).toBeGreaterThanOrEqual(0)
      expect(opened.x + opened.width).toBeLessThanOrEqual(width)
      expect(opened.y + opened.height).toBeLessThanOrEqual(height)
    })
  }
}
