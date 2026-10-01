import { expect, test, type Locator, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'
import { scouting, scoutingFixture } from './scouting-fixture'

// Floating windows open clear of the step being read, its neighbours and each
// other (archon-n7u.4), on the canvas and in Flow, at 1920 and 2560 wide.

type Node = { id: string; title: string }
type Box = { x: number; y: number; width: number; height: number }

const board = scouting.mission as { inputCards: Node[]; formations: Node[]; gates: Node[]; ends: Node[]; connections: Array<{ from: string; to: string }> }
const nodes: Node[] = [...board.inputCards, ...board.formations, ...board.gates, ...board.ends]
const idOf = (title: string) => nodes.find(node => node.title === title)!.id
const neighbours = (nodeId: string) => {
  const found = new Set<string>()
  for (const connection of board.connections) {
    const from = connection.from.split(':')[0]
    const to = connection.to.split(':')[0]
    if (from === nodeId) found.add(to)
    if (to === nodeId) found.add(from)
  }
  found.delete(nodeId)
  return [...found]
}
const overlap = (a: Box, b: Box) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height

/** Whether a click at the element's centre reaches it, rather than a window over it. Elements scrolled out of view count as clear. */
async function clickable(locator: Locator): Promise<boolean> {
  return locator.evaluate(element => {
    const rect = element.getBoundingClientRect()
    if (rect.bottom < 0 || rect.top > innerHeight || rect.right < 0 || rect.left > innerWidth) return true
    const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
    return Boolean(hit && (hit === element || element.contains(hit)))
  })
}

async function windowBoxes(page: Page): Promise<Box[]> {
  return page.locator('.fwin').evaluateAll(windows => windows.map(element => {
    const { x, y, width, height } = element.getBoundingClientRect()
    return { x, y, width, height }
  }))
}

/** Every open window leaves every other window's title bar uncovered, and none opens exactly on another. */
async function expectCascadedOrApart(page: Page) {
  const boxes = await windowBoxes(page)
  boxes.forEach((box, index) => {
    for (const other of boxes.slice(index + 1)) {
      expect(overlap(other, { ...box, height: 40 }), 'a later window covers an earlier title bar').toBe(false)
      expect(Math.abs(other.x - box.x) + Math.abs(other.y - box.y)).toBeGreaterThan(16)
    }
  })
}

for (const [width, height] of [[1920, 1080], [2560, 1440]]) {
  test(`canvas windows at ${width} open clear of their card, its neighbours and each other`, async ({ page }) => {
    await page.setViewportSize({ width, height })
    await page.addInitScript(() => localStorage.clear())
    await scoutingFixture(page)
    await page.goto('/?mission=scouting')
    await expect(page.locator('.formation').first()).toBeVisible()
    await page.getByTitle('Fit', { exact: true }).click()
    await page.waitForTimeout(400)

    for (const title of ['Map the territory', 'Draft the brief', 'Brief sign-off']) {
      const nodeId = idOf(title)
      await page.locator(`[data-node="${nodeId}"]`).getByText(title, { exact: true }).first().click()
      await expect(page.locator(`[data-window-id="node:${nodeId}"]`)).toBeVisible()
      for (const id of [nodeId, ...neighbours(nodeId)]) {
        const card = page.locator(`.formation[data-node="${id}"], .gatecard[data-node="${id}"], .missioncard[data-node="${id}"], .endcard[data-node="${id}"]`).first()
        expect(await clickable(card), `${title}: ${nodes.find(node => node.id === id)?.title} stays clickable`).toBe(true)
      }
    }
    // The click the reading pass aimed after opening three windows reaches Adversarial review's card.
    expect(await clickable(page.locator(`[data-node="${idOf('Adversarial review')}"]`).first())).toBe(true)
    await expectCascadedOrApart(page)
    await page.screenshot({ path: test.info().outputPath(`canvas-three-windows-${width}.png`) })

    // A note window opens clear of its card and the windows already open.
    const title = 'Map the territory'
    await page.getByRole('button', { name: `Open notes for ${title}` }).click()
    await expect(page.getByRole('dialog', { name: `notes for ${title}` })).toBeVisible()
    expect(await clickable(page.locator(`[data-node="${idOf(title)}"]`).first())).toBe(true)
    await expectCascadedOrApart(page)
  })

  test(`Flow windows at ${width} leave each row's number, title and links clickable`, async ({ page }) => {
    await page.setViewportSize({ width, height })
    await page.addInitScript(() => localStorage.clear())
    await scoutingFixture(page)
    await page.goto('/?mission=scouting')
    await page.getByRole('radio', { name: 'Flow' }).click()
    const flow = page.getByTestId('flow-view')
    await expect(flow).toBeVisible()

    const steps = ['Map the territory', 'Framing review', 'Question peers']
    for (const [index, title] of steps.entries()) {
      const nodeId = idOf(title)
      await page.getByRole('button', { name: `${index + 1} ${title}`, exact: true }).click()
      await expect(page.locator(`[data-window-id="node:${nodeId}"]`)).toBeVisible()
      for (const id of [nodeId, ...neighbours(nodeId)]) {
        const row = flow.locator(`.flow-step[data-flow-node="${id}"], .flow-mission[data-flow-node="${id}"]`)
        if (!await row.count()) continue
        for (const handle of await row.locator('.flow-number, .flow-step-head .flow-title, .flow-mission-head .flow-title, .flow-link').all()) {
          expect(await clickable(handle), `${title}: ${await handle.innerText()} stays clickable`).toBe(true)
        }
      }
    }
    await expectCascadedOrApart(page)
    if (width >= 2560) {
      // At 2560 the gutters hold a window each: none lies on the column.
      const column = (await flow.locator('.flow-step').first().boundingBox())!
      for (const box of await windowBoxes(page)) expect(overlap(box, column)).toBe(false)
    }
    await page.screenshot({ path: test.info().outputPath(`flow-three-windows-${width}.png`) })
  })
}

test('the run bar\'s produced menu opens above a file window, and the next file opens clear of it', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser&run=run_browser')

  const produced = page.getByTestId('run-produced')
  await produced.getByRole('button', { name: 'review.md' }).click()
  const review = page.getByRole('dialog', { name: 'file review.md' })
  await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()

  // The file window opens just below the run bar, where the menu drops; the menu still wins.
  await produced.getByRole('button', { name: '3 more produced files' }).click()
  const item = page.getByRole('menuitem', { name: 'worker.log' })
  await expect(item).toBeVisible()
  expect(await clickable(item)).toBe(true)
  await item.click()
  const log = page.getByRole('dialog', { name: 'file worker.log' })
  await expect(log).toContainText('worker finished')
  expect(overlap((await review.boundingBox())!, (await log.boundingBox())!)).toBe(false)
})
