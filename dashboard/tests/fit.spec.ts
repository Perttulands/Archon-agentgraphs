import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'
import { transitionsSettled } from './settled'

test('Arrange followed immediately by Fit keeps the disconnected gate accessible', async ({ page }) => {
  await cockpitFixture(page, { far: true })
  await page.goto('/')
  await expect(page.locator('[data-node="loose"]')).toBeVisible()
  await page.getByTestId('arrange-layout').click()
  await page.waitForFunction(() => document.getAnimations().some(a => (a as CSSTransition).transitionProperty === 'left'))
  // Deliberately press during Arrange's animation, without Playwright waiting
  // for the moving target to settle. This is the reported operator sequence.
  await page.getByTitle('Fit', { exact: true }).evaluate((el: HTMLButtonElement) => el.click())
  await transitionsSettled(page)
  const bounds = await page.locator('[data-node="loose"]').evaluate(el => {
    const card = el.getBoundingClientRect()
    const viewport = document.querySelector('.fmx .viewport')!.getBoundingClientRect()
    const hit = document.elementFromPoint(card.x + card.width / 2, card.y + card.height / 2)
    return { left: card.left, viewportLeft: viewport.left, accessible: !!hit?.closest('[data-node="loose"]') }
  })
  expect(bounds.left).toBeGreaterThanOrEqual(bounds.viewportLeft)
  expect(bounds.accessible).toBe(true)
  await page.screenshot({ path: '/tmp/archon-nno-after.png' })
})

for (const [width, height] of [[1920, 1080], [2560, 1080], [2560, 1440]]) test(`TOML mission opens arranged and Fit is read-only at ${width}x${height}`, async ({ page }) => {
  await page.setViewportSize({ width, height })
  const fixture = await cockpitFixture(page, { noLayout: true })
  await page.goto('/')
  await expect(page.locator('[data-node="lim_cap"]')).toBeVisible()
  // Opening keeps readable text; the explicit Fit action gives the overview.
  await transitionsSettled(page)
  const cards = await page.locator('.world [data-node]').evaluateAll(elements => elements.map(el => {
    const rect = el.getBoundingClientRect()
    return { id: el.getAttribute('data-node'), x: rect.x, y: rect.y, right: rect.right, bottom: rect.bottom }
  }))
  for (let i = 0; i < cards.length; i++) {
    for (const other of cards.slice(i + 1)) {
      const card = cards[i]
      expect(card.right <= other.x || other.right <= card.x || card.bottom <= other.y || other.bottom <= card.y,
        `${card.id} overlaps ${other.id}`).toBe(true)
    }
  }
  const at = (id: string) => cards.find(card => card.id === id)!
  expect(at('mission').x).toBeLessThan(at('execution').x)
  expect(at('execution').x).toBeLessThan(at('gate').x)
  expect(at('gate').x).toBeLessThan(at('peer').x)
  expect(at('judge').x).toBe(at('gate').x)
  expect(at('lim_cap').x).toBe(at('execution').x)
  const authoredPositions = () => page.locator('.world [data-node]').evaluateAll(elements => elements.map(element => ({ id: (element as HTMLElement).dataset.node, left: (element as HTMLElement).style.left, top: (element as HTMLElement).style.top })))
  const positionsBeforeFit = await authoredPositions()
  const mainText = page.locator('[data-node="execution"] .slot-caption').first()
  expect(await mainText.evaluate(element => {
    const world = element.closest('.world')!
    const matrix = new DOMMatrix(getComputedStyle(world).transform)
    return parseFloat(getComputedStyle(element).fontSize) * Math.hypot(matrix.a, matrix.b)
  })).toBeGreaterThanOrEqual(11)
  const title = page.locator('[data-node="gate"] .gt')
  const titleBounds = await title.boundingBox()
  const chipBounds = await page.locator('[data-node="gate"] .gkind-formation').boundingBox()
  expect(titleBounds!.y + titleBounds!.height).toBeLessThanOrEqual(chipBounds!.y)
  expect(await title.evaluate(el => el.scrollHeight <= el.clientHeight)).toBe(true)
  await page.getByTitle('Fit', { exact: true }).click()
  await transitionsSettled(page)
  const viewport = await page.locator('.viewport').boundingBox()
  const overview = await page.locator('.world [data-node]').evaluateAll(elements => elements.map(element => {
    const box = element.getBoundingClientRect()
    return { left: box.left, right: box.right, top: box.top, bottom: box.bottom }
  }))
  for (const card of overview) {
    expect(card.left).toBeGreaterThanOrEqual(viewport!.x)
    expect(card.right).toBeLessThanOrEqual(viewport!.x + viewport!.width)
    expect(card.top).toBeGreaterThanOrEqual(viewport!.y)
    expect(card.bottom).toBeLessThanOrEqual(viewport!.y + viewport!.height)
  }
  expect(await authoredPositions()).toEqual(positionsBeforeFit)
  expect(fixture.writes).toEqual([])
  expect(fixture.board().rev).toBe(1)
  await page.screenshot({ path: `/tmp/canvas-arranged-${width}x${height}.png` })
})
