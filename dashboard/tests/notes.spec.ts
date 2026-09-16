import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Entry = { id: string; author: string; text: string }
const titleOf = (nodeId: string): string => [
  ...wayfinding.board.missions, ...wayfinding.board.formations, ...wayfinding.board.gates,
].find((node: { id: string }) => node.id === nodeId)?.title
const elementNotes: { nodeId: string; entries: Entry[] }[] = wayfinding.notes.elements

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
})

test('no card covers any note preview on Wayfinding', async ({ page }) => {
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  await expect(page.getByRole('note')).toHaveCount(elementNotes.length)
  await page.getByTitle('Fit', { exact: true }).click()
  await page.waitForTimeout(500)

  for (const { nodeId } of elementNotes) {
    const sticky = page.getByRole('note', { name: `Notes for ${titleOf(nodeId)}` })
    await expect(sticky).toBeVisible()
    const covered = await sticky.evaluate(element => {
      const box = element.getBoundingClientRect()
      const points = [[0.2, 0.3], [0.5, 0.5], [0.8, 0.7]].map(([x, y]) => [box.left + box.width * x, box.top + box.height * y])
      return points.filter(([x, y]) => !element.contains(document.elementFromPoint(x, y))).length
    })
    expect(covered, `${titleOf(nodeId)} sticky is covered`).toBe(0)
    expect(await sticky.locator('.note-sticky-text').first().evaluate(element => parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(12)
  }
  expect(fixture.writes).toEqual([])
})

test('full note text on Wayfinding reads on the canvas and in note windows, with no notepad', async ({ page }) => {
  await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  await expect(page.getByRole('complementary', { name: 'Shared board notepad' })).toHaveCount(0)
  await page.getByRole('radio', { name: 'Full notes' }).click()

  for (const { nodeId, entries } of elementNotes) {
    const sticky = page.getByRole('note', { name: `Notes for ${titleOf(nodeId)}` })
    for (const entry of entries) await expect(sticky).toContainText(entry.text)
  }

  const [first] = elementNotes
  await page.getByRole('button', { name: `Open the note thread for ${titleOf(first.nodeId)}` }).click()
  const noteWindow = page.getByRole('dialog', { name: `notes for ${titleOf(first.nodeId)}` })
  for (const entry of first.entries) await expect(noteWindow.getByTestId(`note-entry-${entry.id}`)).toContainText(entry.text)
  const entryText = noteWindow.locator('.note-entry-text').first()
  expect(await entryText.evaluate(element => element.scrollHeight <= element.clientHeight + 1)).toBe(true)

  await page.getByRole('button', { name: 'Board notes' }).click()
  const boardWindow = page.getByRole('dialog', { name: 'board notes' })
  for (const entry of wayfinding.notes.board as Entry[]) await expect(boardWindow).toContainText(entry.text)
  await expect(noteWindow).toBeVisible()
})
