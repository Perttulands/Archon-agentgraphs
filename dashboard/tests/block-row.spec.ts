import { expect, test, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

/* The row under the top bar holds the mission's building blocks while it is
 * authored, and the run bar while a run is open (archon-kiwt.4). A plain click
 * on bare canvas puts every floating window away, and a card's window replaces
 * the open one with what was opened from it (archon-kiwt.2). */

test.use({ viewport: { width: 1920, height: 1080 } })

// A click on a card's title, delivered to the card even where a window lies over it.
async function clickCard(page: Page, nodeId: string) {
  await page.locator(`.formation[data-node="${nodeId}"] .tt`).evaluate(title => {
    const box = title.getBoundingClientRect()
    const at = { bubbles: true, button: 0, pointerId: 31, clientX: box.left + 4, clientY: box.top + 4 }
    title.dispatchEvent(new PointerEvent('pointerdown', at))
    window.dispatchEvent(new PointerEvent('pointerup', at))
  })
}

// A point of the canvas with nothing on it, as the browser hit-tests it.
async function bareCanvas(page: Page) {
  const point = await page.evaluate(() => {
    const viewport = document.querySelector('.viewport')!.getBoundingClientRect()
    for (let y = viewport.top + 40; y < viewport.bottom - 40; y += 24) {
      for (let x = viewport.left + 40; x < viewport.right - 200; x += 24) {
        const hit = document.elementFromPoint(x, y)
        if (hit && (hit.matches('.viewport, .world') || hit.matches('svg.wires'))) return { x, y }
      }
    }
    return null
  })
  expect(point).not.toBeNull()
  return point!
}

test('while authoring the row holds Formation, Gate, End and Limit, and the canvas has no dock or roles roster', async ({ page }) => {
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const row = page.getByTestId('block-row')
  await expect(row).toBeVisible()
  const blocks = row.locator('.block')
  await expect(blocks).toHaveText(['Formation', 'Gate', 'End', 'Limit'])
  for (const block of await blocks.all()) await expect(block.locator('svg')).toBeVisible()
  // The row sits in the band under the top bar, and the zoom, Arrange and Fit controls stay bottom right.
  const band = (await page.locator('.run-banner-space').boundingBox())!
  const box = (await row.boundingBox())!
  expect(box.y).toBeGreaterThanOrEqual(band.y)
  expect(box.y + box.height).toBeLessThanOrEqual(band.y + band.height + 1)
  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  const zoom = (await page.locator('.zoomctl').boundingBox())!
  expect(zoom.x + zoom.width).toBeGreaterThan(canvas.x + canvas.width - 80)
  expect(zoom.y + zoom.height).toBeGreaterThan(canvas.y + canvas.height - 80)
  await expect(page.locator('.dock')).toHaveCount(0)
  await expect(page.getByTestId('roles-toggle')).toHaveCount(0)
  await expect(page.getByTestId('agent-roster')).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('block-row.png') })

  // A click on Formation adds one, named in its window.
  await row.getByRole('button', { name: 'Formation' }).click()
  await expect.poll(() => fixture.board().formations.length).toBe(4)
  await expect(page.locator('.formation')).toHaveCount(4)
  await expect(page.getByRole('dialog', { name: /New formation/ })).toBeVisible()
  // A drag of Gate onto bare canvas opens the new gate there.
  await page.keyboard.press('Escape')
  const from = (await row.getByTestId('gate-token').boundingBox())!
  const to = await bareCanvas(page)
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(to.x, to.y, { steps: 8 })
  await page.mouse.up()
  await expect(page.getByRole('dialog', { name: 'Create gate' })).toBeVisible()
})

test('a run turns the row into the run bar, a card replaces the open card window with what it opened, and a click outside puts all away', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser&run=run_browser')
  const banner = page.getByTestId('run-banner')
  await expect(banner.locator('.badge')).toHaveText('Succeeded')
  await expect(page.getByTestId('block-row')).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('run-bar.png') })

  // Peer review's window, its attempts and a file from those.
  await page.locator('.formation[data-node="peer"] .tt').click()
  const peer = page.getByRole('dialog', { name: 'Formation · Peer review' })
  await peer.getByTestId('attempts-open-peer').click()
  const attempts = page.getByRole('dialog', { name: 'Attempts of Peer review' })
  await attempts.getByRole('button', { name: 'review.md', exact: true }).click()
  const file = page.getByRole('dialog', { name: /review\.md/ })
  await expect(file).toBeVisible()

  // Another card's window replaces Peer review's, and what Peer review opened closes with it.
  await clickCard(page, 'execution')
  const execution = page.getByRole('dialog', { name: 'Formation · Execution' })
  await expect(execution).toBeVisible()
  await expect(peer).toHaveCount(0)
  await expect(attempts).toHaveCount(0)
  await expect(file).toHaveCount(0)

  // A drag pans and a press in a window is not outside; both keep the windows.
  await page.locator('.formation[data-node="execution"]').getByTestId('attempts-open-execution').click()
  const executionAttempts = page.getByRole('dialog', { name: 'Attempts of Execution' })
  await expect(executionAttempts).toBeVisible()
  const spot = await bareCanvas(page)
  await page.mouse.move(spot.x, spot.y)
  await page.mouse.down()
  await page.mouse.move(spot.x + 60, spot.y + 30, { steps: 5 })
  await page.mouse.up()
  await execution.locator('.fwin-body').click({ position: { x: 10, y: 10 } })
  await expect(execution).toBeVisible()
  await expect(executionAttempts).toBeVisible()

  const bare = await bareCanvas(page)
  await page.mouse.click(bare.x, bare.y)
  await expect(execution).toHaveCount(0)
  await expect(executionAttempts).toHaveCount(0)
  await expect(page.locator('.fwin')).toHaveCount(0)
})
