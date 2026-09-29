import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

test('three solo outputs join one input and one undo removes the last join', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { join: true })
  await page.goto('/?board=browser')
  const sink = page.getByTestId('formation-node-sink')
  await expect(sink).toBeVisible()
  for (const [index, id] of ['a', 'b', 'c'].entries()) {
    const source = await page.locator(`[data-port-out="${id}:out"]`).boundingBox()
    const target = await page.locator('[data-port-in="sink:in"]').boundingBox()
    expect(source).not.toBeNull()
    expect(target).not.toBeNull()
    await page.mouse.move(source!.x + source!.width / 2, source!.y + source!.height / 2)
    await page.mouse.down()
    await page.mouse.move(target!.x + target!.width / 2, target!.y + target!.height / 2, { steps: 12 })
    await page.mouse.up()
    await expect(sink.locator('[data-port-in].has')).toHaveCount(index + 1)
    await expect(page.getByTestId('formations-error')).toHaveCount(0)
  }
  await expect(sink.locator('.fio.in')).toHaveCount(3)
  expect(fixture.board().connections).toHaveLength(3)
  expect(fixture.board().rev).toBe(4)
  await page.screenshot({ path: test.info().outputPath('three-solo-join.png') })
  await page.keyboard.press('Control+z')
  await expect(sink.locator('[data-port-in]')).toHaveCount(2)
  await expect(sink.locator('[data-port-in].has')).toHaveCount(2)
  expect(fixture.board().connections.map(edge => edge.from)).toEqual(['a:out', 'b:out'])
  expect(fixture.board().rev).toBe(5)
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
})
