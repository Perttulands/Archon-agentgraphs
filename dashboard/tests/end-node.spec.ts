import { expect, test, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

// End nodes end a path on purpose (form-o7p.10): created from the End token and
// the canvas menu, wired from gate routes (several into one), switched between
// done and rejected, deleted and restored, each step with undo.

// Screenshots go to the test's own output directory (test-results/), so the
// spec stays host-neutral.
async function shot(page: Page, name: string) {
  await page.screenshot({ path: test.info().outputPath(name) })
}

async function center(page: Page, selector: string) {
  const box = await page.locator(selector).boundingBox()
  expect(box, selector).not.toBeNull()
  return { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 }
}

async function drag(page: Page, from: { x: number; y: number }, to: { x: number; y: number }) {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(to.x, to.y, { steps: 12 })
  await page.mouse.up()
}

async function wire(page: Page, from: string, to: string) {
  await drag(page, await center(page, `[data-port-out="${from}"]`), await center(page, `[data-port-in="${to}"]`))
}

test('End nodes are created, wired, switched, deleted and restored on the canvas with undo', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const loose = page.getByTestId('gate-node-loose')
  await expect(loose).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })
  const ends = page.locator('.endcard')
  const box = (await loose.boundingBox())!
  const spot = (dx: number, dy: number) => ({ x: box.x + box.width + dx, y: box.y + dy })

  // Drag the End token from the top bar onto the canvas: a Done End node.
  await drag(page, await center(page, '[data-testid="end-token"]'), spot(70, -10))
  await expect(ends).toHaveCount(1)
  const done = fixture.board().ends![0]
  expect(done).toMatchObject({ title: 'Done', outcome: 'done' })
  const doneCard = page.getByTestId(`end-node-${done.id}`)
  await expect(doneCard).toHaveClass(/end-done/)
  await expect(doneCard).toContainText('End · done')

  // The canvas menu makes a rejected one.
  // The empty canvas above the disconnected gate.
  await page.mouse.click(box.x + 110, box.y - 330, { button: 'right' })
  await menuItem('End node · rejected').click()
  await expect(ends).toHaveCount(2)
  const rejected = fixture.board().ends![1]
  expect(rejected).toMatchObject({ title: 'Rejected', outcome: 'rejected' })
  const rejectedCard = page.getByTestId(`end-node-${rejected.id}`)
  await expect(rejectedCard).toHaveClass(/end-rejected/)

  // Undo takes back a creation.
  await page.mouse.click(box.x + 110, box.y - 200, { button: 'right' })
  await menuItem('End node · done').click()
  await expect(ends).toHaveCount(3)
  await page.keyboard.press('Control+z')
  await expect(ends).toHaveCount(2)
  expect(fixture.board().ends!.map(end => end.id)).toEqual([done.id, rejected.id])

  // Gate routes end there; two routes may share one End node.
  await wire(page, 'loose:pass', `${done.id}:in`)
  await wire(page, 'loose:fail', `${rejected.id}:in`)
  await wire(page, 'gate:fail', `${rejected.id}:in`)
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
  const into = (endId: string) => fixture.board().connections.filter(edge => edge.to === `${endId}:in`).map(edge => edge.from).sort()
  expect(into(done.id)).toEqual(['loose:pass'])
  expect(into(rejected.id)).toEqual(['gate:fail', 'loose:fail'])
  await expect(rejectedCard.locator('[data-port-in].has')).toHaveCount(1)
  await shot(page, 'end-nodes-wired.png')

  // Undo takes back the last wire, and wiring again restores it.
  await page.keyboard.press('Control+z')
  await expect.poll(() => into(rejected.id)).toEqual(['loose:fail'])
  await wire(page, 'gate:fail', `${rejected.id}:in`)
  await expect.poll(() => into(rejected.id)).toEqual(['gate:fail', 'loose:fail'])

  // The End node's menu switches its outcome; a default title follows it.
  await doneCard.click({ button: 'right' })
  await menuItem('End rejected instead').click()
  await expect(doneCard).toHaveClass(/end-rejected/)
  expect(fixture.board().ends![0]).toMatchObject({ title: 'Rejected', outcome: 'rejected' })

  // Its window switches it back.
  await doneCard.click()
  await expect(page.getByRole('dialog', { name: 'End node · Rejected' })).toBeVisible()
  // The window follows the node as its default title changes with the outcome.
  const window = page.getByRole('dialog').filter({ has: page.getByTestId(`node-window-${done.id}`) })
  await expect(window.getByRole('list').first()).toContainText("Ends 4 Disconnected gate's pass route")
  await window.getByRole('radio', { name: 'Done' }).click()
  await expect(doneCard).toHaveClass(/end-done/)
  await expect(window.getByRole('radio', { name: 'Done' })).toHaveAttribute('aria-checked', 'true')
  expect(fixture.board().ends![0]).toMatchObject({ title: 'Done', outcome: 'done' })
  await expect(page.getByRole('dialog', { name: 'End node · Done' })).toBeVisible()
  await shot(page, 'end-node-window.png')
  await window.getByRole('button', { name: /close/i }).first().click()

  // The gate's window says where each route ends.
  await loose.click()
  const gateWindow = page.getByRole('dialog', { name: 'Gate · Disconnected gate' })
  await expect(gateWindow.getByRole('button', { name: 'Pass → this path ends (done)' })).toBeVisible()
  await expect(gateWindow.getByRole('button', { name: 'Fail → this path ends (rejected)' })).toBeVisible()
  await gateWindow.getByRole('button', { name: /close/i }).first().click()

  // Delete from its menu; undo brings it back with both routes into it.
  await rejectedCard.click({ button: 'right' })
  await menuItem('Delete End node').click()
  await expect(rejectedCard).toHaveCount(0)
  expect(into(rejected.id)).toEqual([])
  await page.keyboard.press('Control+z')
  await expect(page.getByTestId(`end-node-${rejected.id}`)).toBeVisible()
  await expect.poll(() => into(rejected.id)).toEqual(['gate:fail', 'loose:fail'])
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
  await shot(page, 'end-node-restored.png')

  // The Flow view reads where each path ends.
  await page.getByRole('radio', { name: 'Flow' }).click()
  const flow = page.getByTestId('flow-view')
  await expect(flow.getByTestId('flow-step-loose')).toContainText('Pass→ this path ends (done)')
  await expect(flow.getByTestId('flow-step-loose')).toContainText('Fail→ this path ends (rejected)')
  await expect(flow.getByTestId('flow-step-gate')).toContainText('Fail→ this path ends (rejected)')
  await shot(page, 'end-nodes-flow.png')
})
