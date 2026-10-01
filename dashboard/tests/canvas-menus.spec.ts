import { expect, test, type Locator, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

test.use({ viewport: { width: 1920, height: 1080 } })

async function expectOnScreen(page: Page, menu: Locator) {
  const box = await menu.boundingBox()
  const viewport = page.viewportSize()!
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height)
}

test('the canvas menu stays on screen from every corner of the canvas', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page)
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('formation-node-peer')).toBeVisible()
  const viewport = await page.locator('.viewport').boundingBox()
  const menu = page.locator('.ctxmenu')
  let opened = 0
  const widths = new Set<number>()
  for (const [fx, fy] of [[0.02, 0.02], [0.98, 0.02], [0.02, 0.98], [0.98, 0.98], [0.5, 0.9]]) {
    const x = viewport!.x + viewport!.width * fx
    const y = viewport!.y + viewport!.height * fy
    // Right-click empty canvas only: skip points that land on a card.
    const onCard = await page.evaluate(([px, py]) => Boolean(document.elementFromPoint(px, py)?.closest('.formation,.gatecard,.missioncard,.zoomctl')), [x, y])
    if (onCard) continue
    await page.mouse.click(x, y, { button: 'right' })
    await expect(menu).toBeVisible()
    await expectOnScreen(page, menu)
    // Measured before placement where nothing narrows it, the menu keeps one width at every edge.
    widths.add(Math.round((await menu.boundingBox())!.width))
    await expect(menu.getByRole('menuitem').last()).toBeInViewport()
    await page.keyboard.press('Escape')
    await expect(menu).toHaveCount(0)
    opened++
  }
  expect(opened).toBeGreaterThanOrEqual(4)
  expect([...widths]).toHaveLength(1)
})

test('a long roster\'s role list scrolls inside the staffing window, which stays on screen, and its last role is one click away', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  // The window grows to show a whole catalog up to 640 px; a roster this long scrolls inside it.
  const fixture = await cockpitFixture(page, { extraAgents: 70 })
  await page.goto('/?mission=browser')
  const slot = page.getByTestId('slot-execution-worker')
  await expect(slot).toBeVisible()
  await slot.locator('[data-part=role]').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  await expectOnScreen(page, sentence)
  const roles = sentence.getByRole('listbox', { name: 'Choose role' })
  await expect(roles.getByRole('option')).toHaveCount(73)
  await page.screenshot({ path: test.info().outputPath('long-role-list.png') })

  // The list scrolls inside the window, which stays where it opened.
  expect(await roles.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
  const last = roles.getByRole('option', { name: /Roster agent 70/ })
  await last.scrollIntoViewIfNeeded()
  await expect(last).toBeInViewport()
  // vanilla stays pinned at the top of the scrolled grid, one click away.
  const [list, vanilla] = [(await roles.boundingBox())!, (await roles.locator('[data-row="vanilla"]').boundingBox())!]
  expect(vanilla.y).toBeGreaterThanOrEqual(list.y - 1)
  expect(vanilla.y + vanilla.height).toBeLessThanOrEqual(list.y + 40)
  await expectOnScreen(page, sentence)
  await last.click()
  await expect(sentence).toHaveCount(0)
  await expect(slot.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Roster agent 70 | Codex · default model · medium')
  expect(fixture.patches.map(patch => patch.assignSlot).filter(Boolean)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: 'agent-70', harness: 'openai-codex', model: '', effort: 'medium' }])
})
