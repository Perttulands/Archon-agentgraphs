import { expect, test } from '@playwright/test'
import { scouting, scoutingFixture } from './scouting-fixture'
import { transitionsSettled } from './settled'

const CARDS = '.formation[data-node], .gatecard[data-node], .missioncard[data-node], .toolcard[data-node]'

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
  await expect(page.getByRole('note')).toHaveCount(elementNotes.length)
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

test('full note text on Scouting reads on the canvas and in note windows, with no notepad', async ({ page }) => {
  await scoutingFixture(page)
  await page.goto('/?mission=scouting')
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

  await page.getByRole('button', { name: 'Mission notes' }).click()
  const boardWindow = page.getByRole('dialog', { name: 'mission notes' })
  for (const entry of scouting.notes.mission as Entry[]) await expect(boardWindow).toContainText(entry.text)
  await expect(noteWindow).toBeVisible()
})

test('a note window opens beside its node, leaving the card and its note in view', async ({ page }) => {
  await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  await expect(page.getByRole('note')).toHaveCount(elementNotes.length)
  await page.getByTitle('Fit', { exact: true }).click()
  await transitionsSettled(page)

  // The first step has room on its right; the last sits at the board's right edge.
  for (const { nodeId } of [elementNotes[0], elementNotes[elementNotes.length - 1]]) {
    const title = titleOf(nodeId)
    const card = await page.locator(`[data-node="${nodeId}"]`).first().boundingBox()
    const sticky = await page.getByRole('note', { name: `Notes for ${title}` }).boundingBox()
    await page.getByRole('button', { name: `Open notes for ${title}` }).click()
    const noteWindow = page.getByRole('dialog', { name: `notes for ${title}` })
    await expect(noteWindow).toBeVisible()
    const win = (await noteWindow.boundingBox())!
    const idea = {
      left: Math.min(card!.x, sticky!.x), top: Math.min(card!.y, sticky!.y),
      right: Math.max(card!.x + card!.width, sticky!.x + sticky!.width), bottom: Math.max(card!.y + card!.height, sticky!.y + sticky!.height),
    }
    const gap = Math.max(win.x - idea.right, idea.left - (win.x + win.width), win.y - idea.bottom, idea.top - (win.y + win.height))
    expect(gap, `${title} window covers its card or note`).toBeGreaterThanOrEqual(0)
    // It opens in the free space nearest its node, so it may sit a little way off rather than over a neighbour.
    expect(gap, `${title} window opens away from its node`).toBeLessThanOrEqual(240)
    for (const other of await page.locator(CARDS).all()) {
      const box = await other.boundingBox()
      if (!box) continue
      const covers = win.x < box.x + box.width && box.x < win.x + win.width && win.y < box.y + box.height && box.y < win.y + win.height
      expect(covers, `${title} window covers a card`).toBe(false)
    }
    await noteWindow.getByRole('button', { name: `Close notes for ${title}` }).click()
  }
})
