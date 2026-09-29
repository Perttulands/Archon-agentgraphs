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
  await page.goto('/?board=browser')
  await expect(page.getByTestId('formation-node-peer')).toBeVisible()
  const viewport = await page.locator('.viewport').boundingBox()
  const menu = page.locator('.ctxmenu')
  let opened = 0
  for (const [fx, fy] of [[0.02, 0.02], [0.98, 0.02], [0.02, 0.98], [0.98, 0.98], [0.5, 0.9]]) {
    const x = viewport!.x + viewport!.width * fx
    const y = viewport!.y + viewport!.height * fy
    // Right-click empty canvas only: skip points that land on a card.
    const onCard = await page.evaluate(([px, py]) => Boolean(document.elementFromPoint(px, py)?.closest('.formation,.gatecard,.missioncard,.zoomctl')), [x, y])
    if (onCard) continue
    await page.mouse.click(x, y, { button: 'right' })
    await expect(menu).toBeVisible()
    await expectOnScreen(page, menu)
    await expect(menu.getByRole('menuitem').last()).toBeInViewport()
    await page.keyboard.press('Escape')
    await expect(menu).toHaveCount(0)
    opened++
  }
  expect(opened).toBeGreaterThanOrEqual(4)
})

test('a long Assign agent menu fits the screen, scrolls to its last agent, and its section head matches the menu', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page, { extraAgents: 40 })
  await page.goto('/?board=browser')
  const slot = page.getByTestId('slot-execution-worker')
  await expect(slot).toBeVisible()
  await slot.click({ button: 'right' })
  const menu = page.locator('.ctxmenu')
  await expect(menu).toBeVisible()
  await expectOnScreen(page, menu)
  expect(await menu.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)

  const head = menu.locator('.msection', { hasText: 'Assign agent' })
  const [headStyle, titleStyle] = await Promise.all([head, menu.locator('.mhead')].map(locator => locator.evaluate(el => {
    const style = getComputedStyle(el)
    return { fontSize: style.fontSize, transform: style.textTransform, color: style.color, letterSpacing: style.letterSpacing }
  })))
  expect(headStyle).toEqual(titleStyle)
  await page.screenshot({ path: test.info().outputPath('assign-agent-menu-top.png') })

  const last = menu.getByRole('menuitem', { name: 'Roster agent 40' })
  await last.scrollIntoViewIfNeeded()
  await expect(last).toBeInViewport()
  await expectOnScreen(page, menu)
  await page.screenshot({ path: test.info().outputPath('assign-agent-menu-scrolled.png') })
  await last.click()
  await expect(menu).toHaveCount(0)
})
