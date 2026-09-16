import { expect, test, type Locator } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

type Box = { x: number; y: number; width: number; height: number }
const intersects = (a: Box, b: Box) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height

// WCAG contrast between an element's text colour and the first opaque background behind it.
async function textStyle(locator: Locator) {
  return locator.evaluate(element => {
    const rgb = (value: string) => (value.match(/[\d.]+/g) || []).map(Number)
    const luminance = ([r, g, b]: number[]) => {
      const channel = (c: number) => { const v = c / 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4 }
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
    }
    let background = [0, 0, 0]
    for (let node: Element | null = element; node; node = node.parentElement) {
      const color = rgb(getComputedStyle(node).backgroundColor)
      if (color.length === 3 || (color.length === 4 && color[3] === 1)) { background = color.slice(0, 3); break }
    }
    const style = getComputedStyle(element)
    const [light, dark] = [luminance(rgb(style.color).slice(0, 3)), luminance(background)].sort((a, b) => b - a)
    return { fontSize: parseFloat(style.fontSize), contrast: (light + 0.05) / (dark + 0.05), text: element.textContent || '' }
  })
}

test('formation briefs and gate criteria are legible on the card', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page)
  await page.goto('/')
  for (const locator of [
    page.locator('[data-node="execution"] .fhead .tg'),
    page.locator('[data-gate="gate"] .gs'),
  ]) {
    const style = await textStyle(locator)
    expect(style.text.length, 'authored text').toBeGreaterThan(0)
    expect(style.fontSize, `${style.text} font size`).toBeGreaterThanOrEqual(11)
    expect(style.contrast, `${style.text} contrast`).toBeGreaterThanOrEqual(4.5)
  }
  await expect(page.locator('[data-node="execution"] .tg')).not.toHaveClass(/placeholder/)
})

test('the answer panel never overlaps the zoom column at 1440x900', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page, { waitingHuman: true })
  await page.goto('/')
  const panel = page.getByTestId('gate-answer')
  await expect(panel.locator('.gate-answer-text')).toContainText('60. A question')
  const approve = panel.getByRole('button', { name: 'Approve' })
  await expect(approve).toBeInViewport()
  const panelBox = (await panel.boundingBox())!
  const approveBox = (await approve.boundingBox())!
  for (const control of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await control.boundingBox())!
    expect(intersects(panelBox, box), 'panel over zoom controls').toBe(false)
    expect(intersects(approveBox, box), 'Approve under zoom controls').toBe(false)
  }
})

test('Escape closes the cockpit dialogs', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page, { run: true })
  await page.goto('/')

  await page.getByTestId('run-mission-mission').click()
  await expect(page.getByRole('dialog', { name: 'Start mission' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Start mission' })).toHaveCount(0)

  await page.getByTestId('formation-node-execution').locator('.fhead .tt').click()
  await expect(page.getByRole('dialog', { name: 'Formation · Execution' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Formation · Execution' })).toHaveCount(0)

  await page.getByTestId('inspect-node-execution').click()
  await expect(page.getByRole('dialog', { name: 'Run evidence · Execution' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Run evidence · Execution' })).toHaveCount(0)
})

test('the roster resizes, remembers its width and collapses to a rail', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page)
  await page.goto('/')
  const roster = page.getByTestId('agent-roster')
  await expect(roster).toBeVisible()
  expect((await roster.boundingBox())!.width).toBe(236)

  const handle = page.getByRole('separator', { name: 'Resize agent roster' })
  const box = (await handle.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + 300)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 120, box.y + 300, { steps: 5 })
  await page.mouse.up()
  expect((await roster.boundingBox())!.width).toBe(356)
  await page.reload()
  await expect(roster).toBeVisible()
  expect((await roster.boundingBox())!.width).toBe(356)

  const canvasBefore = (await page.getByTestId('formations-canvas').boundingBox())!.width
  await page.getByRole('button', { name: 'Collapse agent roster' }).click()
  expect((await roster.boundingBox())!.width).toBe(40)
  expect((await page.getByTestId('formations-canvas').boundingBox())!.width).toBe(canvasBefore + 316)
  await page.getByRole('button', { name: 'Expand agent roster' }).click()
  expect((await roster.boundingBox())!.width).toBe(356)
})
