import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

test('Arrange followed immediately by Fit keeps the disconnected gate accessible', async ({ page }) => {
  await cockpitFixture(page, { far: true })
  await page.goto('/')
  await expect(page.locator('[data-node="loose"]')).toBeVisible()
  await page.getByTestId('arrange-layout').click()
  await page.waitForFunction(() => document.getAnimations().some(a => (a as CSSTransition).transitionProperty === 'left'))
  // Deliberately press during Arrange's animation, without Playwright waiting
  // for the moving target to settle. This is the reported operator sequence.
  await page.getByTitle('Fit', { exact: true }).evaluate((el: HTMLButtonElement) => el.click())
  await page.waitForTimeout(550)
  const bounds = await page.locator('[data-node="loose"]').evaluate(el => {
    const card = el.getBoundingClientRect()
    const viewport = document.querySelector('.fmx .viewport')!.getBoundingClientRect()
    const hit = document.elementFromPoint(card.x + card.width / 2, card.y + card.height / 2)
    return { left: card.left, viewportLeft: viewport.left, accessible: !!hit?.closest('[data-node="loose"]') }
  })
  expect(bounds.left).toBeGreaterThanOrEqual(bounds.viewportLeft)
  expect(bounds.accessible).toBe(true)
  await page.screenshot({ path: '/tmp/form-nno-after.png' })
})
