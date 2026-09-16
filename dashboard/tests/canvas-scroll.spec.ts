import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Node = { id: string; title: string }

test('the canvas never scrolls natively: card clicks and keyboard focus leave the zoom column at the right edge', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  const canvas = page.getByTestId('formations-canvas')
  await expect(page.locator('.formation').first()).toBeVisible()
  // Zoom in until the Wayfinding chain runs off both sides of the canvas.
  for (let step = 0; step < 4; step++) await page.getByTitle('Zoom in').click()

  const expectUnscrolled = async (after: string) => {
    expect(await canvas.evaluate(element => [element.scrollLeft, element.scrollTop]), `canvas scroll after ${after}`).toEqual([0, 0])
    const canvasBox = (await canvas.boundingBox())!
    const zoom = (await page.locator('.zoomctl').boundingBox())!
    expect(canvasBox.x + canvasBox.width - (zoom.x + zoom.width), `zoom column after ${after}`).toBeCloseTo(18, 0)
  }

  const cards = [
    ...wayfinding.board.formations.map((node: Node) => ({ title: node.title, card: page.getByTestId(`formation-node-${node.id}`).locator('.fhead') })),
    ...wayfinding.board.gates.map((node: Node) => ({ title: node.title, card: page.getByTestId(`gate-node-${node.id}`) })),
  ]
  const canvasBox = (await canvas.boundingBox())!
  let partlyOffScreen = 0
  for (const { title, card } of cards) {
    const box = await card.boundingBox()
    if (!box) continue
    const left = Math.max(box.x, canvasBox.x + 4)
    const right = Math.min(box.x + box.width, canvasBox.x + canvasBox.width - 90)
    if (right - left < 12 || box.y > canvasBox.y + canvasBox.height - 20) continue
    if (box.x < canvasBox.x || box.x + box.width > canvasBox.x + canvasBox.width) partlyOffScreen++
    await page.mouse.click((left + right) / 2, box.y + Math.min(box.height / 2, 30))
    await expect(page.getByRole('dialog', { name: new RegExp(`· ${title}$`) })).toBeVisible()
    await expectUnscrolled(`clicking ${title}`)
    await page.keyboard.press('Escape')
  }
  expect(partlyOffScreen, 'cards clicked while partly off screen').toBeGreaterThan(0)

  // Keyboard focus on card controls, including ones off screen, never scrolls the canvas either.
  const critic = wayfinding.board.formations.find((node: Node) => node.title === 'Brief critic')
  await page.getByTestId(`formation-type-${critic.id}`).focus()
  await expectUnscrolled('focusing an off-screen type chip')
  for (let tab = 0; tab < 12; tab++) {
    await page.keyboard.press('Tab')
    await expectUnscrolled(`Tab ${tab + 1}`)
  }
  expect(fixture.writes).toEqual([])
})
