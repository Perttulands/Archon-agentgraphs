import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'
import { scouting, scoutingFixture } from './scouting-fixture'
import { transitionsSettled } from './settled'

type Entry = { id: string; author: string; text: string }
const titleOf = (nodeId: string): string => [
  ...scouting.mission.inputCards, ...scouting.mission.formations, ...scouting.mission.gates,
].find((node: { id: string }) => node.id === nodeId)?.title
const elementNotes: { nodeId: string; entries: Entry[] }[] = scouting.notes.elements

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
})

test('no card covers any note preview on Scouting', async ({ page }) => {
  const fixture = await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  // One sticky per noted card, and the mission notepad.
  await expect(page.getByRole('note')).toHaveCount(elementNotes.length + 1)
  await page.getByTitle('Fit', { exact: true }).click()
  await transitionsSettled(page)

  for (const { nodeId } of elementNotes) {
    const sticky = page.getByRole('note', { name: `Notes for ${titleOf(nodeId)}` })
    await expect(sticky).toBeVisible()
    const covered = await sticky.evaluate(element => {
      const box = element.getBoundingClientRect()
      const points = [[0.2, 0.3], [0.5, 0.5], [0.8, 0.7]].map(([x, y]) => [box.left + box.width * x, box.top + box.height * y])
      return points.filter(([x, y]) => !element.contains(document.elementFromPoint(x, y))).length
    })
    expect(covered, `${titleOf(nodeId)} sticky is covered`).toBe(0)
    const latest = elementNotes.find(notes => notes.nodeId === nodeId)!.entries.at(-1)!
    await expect(sticky).toHaveClass(new RegExp(`note-sticky-${latest.author.startsWith('agent:') ? 'agent' : 'human'}`))
    expect(await sticky.locator('.note-sticky-text').first().evaluate(element => parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(12)
  }
  expect(fixture.writes).toEqual([])
})

test('every note thread reads in full where it sits on the canvas, the mission notepad too', async ({ page }) => {
  await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  await expect(page.getByRole('radiogroup', { name: 'Notes on the canvas' })).toHaveCount(0)
  const layer = page.getByTestId('note-layer')

  const [first] = elementNotes
  await page.getByRole('button', { name: `Open the note thread for ${titleOf(first.nodeId)}` }).click()
  const opened = layer.getByRole('dialog', { name: `notes for ${titleOf(first.nodeId)}` })
  for (const entry of first.entries) await expect(opened.getByTestId(`note-entry-${entry.id}`)).toContainText(entry.text)

  await page.getByRole('button', { name: 'Mission notes', exact: true }).click()
  const mission = layer.getByRole('dialog', { name: 'mission notes' })
  for (const entry of scouting.notes.mission as Entry[]) await expect(mission).toContainText(entry.text)
  await expect(opened).toBeVisible()
})

test('an open note stays on the canvas: panning and zooming carry it with its card', async ({ page }) => {
  await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  const [first] = elementNotes
  const title = titleOf(first.nodeId)
  await page.getByRole('button', { name: `Open notes for ${title}` }).click()
  const opened = page.getByTestId('note-layer').getByRole('dialog', { name: `notes for ${title}` })
  await expect(opened).toBeVisible()
  const card = page.locator(`[data-node="${first.nodeId}"]`).first()
  const offset = async () => {
    const [c, n] = [(await card.boundingBox())!, (await opened.boundingBox())!]
    return { dx: Math.round(n.x - c.x), dy: Math.round(n.y - c.y) }
  }
  const before = await offset()
  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  await page.mouse.move(canvas.x + 20, canvas.y + canvas.height - 40)
  await page.mouse.down()
  await page.mouse.move(canvas.x + 220, canvas.y + canvas.height - 140, { steps: 8 })
  await page.mouse.up()
  expect(await offset(), 'after a pan').toEqual(before)
  await page.mouse.wheel(0, 200)
  await transitionsSettled(page)
  const zoomed = await offset()
  const scale = await page.locator('.world').evaluate(world => new DOMMatrix(getComputedStyle(world).transform).a)
  expect(Math.abs(zoomed.dx - before.dx * scale), 'after a zoom').toBeLessThanOrEqual(2)
  expect(Math.abs(zoomed.dy - before.dy * scale), 'after a zoom').toBeLessThanOrEqual(2)
})

test('a dragged sticky stays where it was dropped and is saved with the layout', async ({ page }) => {
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const sticky = page.getByRole('note', { name: 'Mission notes' })
  await expect(sticky).toBeVisible()
  const box = (await sticky.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + box.height - 12)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 160, box.y + box.height - 12 + 96, { steps: 8 })
  await page.mouse.up()
  await expect.poll(() => fixture.layoutPatches.some(patch => patch.nodes?.some(node => node.id === 'note:mission'))).toBe(true)
  const moved = (await sticky.boundingBox())!
  expect(Math.abs(moved.x - box.x - 160)).toBeLessThanOrEqual(24)
  expect(Math.abs(moved.y - box.y - 96)).toBeLessThanOrEqual(24)
})
