import { expect, test } from '@playwright/test'
import { scouting, scoutingFixture } from './scouting-fixture'

const CARDS = '.formation[data-node], .gatecard[data-node], .missioncard[data-node], .toolcard[data-node]'

type Node = { id: string; title: string; type?: string; files?: string[]; brief?: { goal?: string; files?: string[] } }
type Box = { x: number; y: number; width: number; height: number }

// How far a window sits from a box: 0 or more when clear of it, negative when it covers it.
const gapBetween = (win: Box, box: Box) =>
  Math.max(win.x - (box.x + box.width), box.x - (win.x + win.width), win.y - (box.y + box.height), box.y - (win.y + win.height))

const RUBRIC = '/srv/projects/scouting/rubrics/adversarial-review.md'

// Scouting with the reference files an operator would attach: a rubric on
// the adversarial review gate, a scoring guide in its judge's brief, and a
// sketch on the mission named by a relative path.
function boardWithFiles() {
  const board = structuredClone(scouting.mission)
  const byTitle = (nodes: Node[], title: string) => nodes.find(node => node.title === title)!
  byTitle(board.inputCards, 'Scouting').files = ['sketch.md']
  byTitle(board.gates, 'Adversarial review').files = [RUBRIC]
  const critic = byTitle(board.formations, 'Brief critic')
  critic.brief = { ...critic.brief, files: ['/home/operator/private/scoring.md'] }
  return board
}

// The daemon's file routes: any absolute path opens (ADR-0021); a relative
// path has no base and returns 400 (src/internal/coordinator/files.go).
const FILES: Record<string, string> = {
  [RUBRIC]: '# Adversarial review rubric\n\nFail a brief whose recommendation would fit any project.',
  '/home/operator/private/scoring.md': '# Scoring guide\n\nScore each brief from one to five.',
}
const servePreview = (route: import('@playwright/test').Route) => {
  const path = new URL(route.request().url()).searchParams.get('path') || ''
  if (!path.startsWith('/')) {
    return route.fulfill({ status: 400, json: { success: false, error: { code: 'Bad Request', message: 'a relative file reference has no base: use an absolute path' } } })
  }
  const text = FILES[path]
  if (text === undefined) return route.fulfill({ status: 404, json: { success: false, error: { code: 'Not Found', message: 'file not found' } } })
  return route.fulfill({ json: { success: true, data: { file: { path, name: path.split('/').pop(), size: text.length, modifiedAt: '', kind: 'markdown', text: { text, bytes: text.length } } } } })
}

// The room Arrange reserves for each card (arrangementItemSize in
// src/internal/formations/layout_arrange.go), with its file chip row.
const FILE_ROW = 30
const reserved = (node: Node, kind: 'inputCard' | 'gate' | 'formation') =>
  (kind === 'inputCard' ? 144 : kind === 'gate' ? 124 : node.type === 'peer' ? 340 : node.type === 'orchestrated' ? 440 : 310) + FILE_ROW

test('a gate\'s rubric and its judge\'s brief file open from the gate on Scouting', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const board = boardWithFiles()
  const fixture = await scoutingFixture(page, { mission: board })
  await page.route('**/api/files/preview?**', servePreview)
  await page.goto('/?mission=scouting')
  await expect(page.getByRole('note')).toHaveCount(scouting.notes.elements.length)

  const cards: Array<[Node, 'inputCard' | 'gate' | 'formation']> = [
    [board.inputCards[0], 'inputCard'],
    [board.gates.find((gate: Node) => gate.title === 'Adversarial review'), 'gate'],
    [board.formations.find((formation: Node) => formation.title === 'Brief critic'), 'formation'],
  ]
  for (const [node, kind] of cards) {
    const card = page.locator(`[data-node="${node.id}"]`).first()
    const chips = card.getByRole('group', { name: 'Referenced files' })
    await expect(chips).toBeVisible()
    expect(await card.evaluate(element => (element as HTMLElement).offsetHeight), `${node.title} card height`).toBeLessThanOrEqual(reserved(node, kind))
    expect(await chips.getByRole('button').first().evaluate(element => parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(11)
  }

  const gate = page.locator(`[data-node="${cards[1][0].id}"]`)
  await gate.getByRole('button', { name: `Open ${RUBRIC}` }).click()
  const rubric = page.getByRole('dialog', { name: 'file adversarial-review.md' })
  await expect(rubric.getByRole('heading', { name: 'Adversarial review rubric' })).toBeVisible()
  await expect(rubric).toContainText(`Adversarial review · ${RUBRIC}`)
  // A chip opens its file beside the card, not the card's node window.
  await expect(page.getByRole('dialog', { name: 'Gate · Adversarial review' })).toHaveCount(0)
  // It opens in the free space nearest the gate: clear of it and of every card, a short way off.
  const rubricBox = (await rubric.boundingBox())!
  const besideGate = gapBetween(rubricBox, (await gate.boundingBox())!)
  expect(besideGate).toBeGreaterThanOrEqual(0)
  expect(besideGate).toBeLessThanOrEqual(240)
  for (const other of await page.locator(CARDS).all()) {
    const box = await other.boundingBox()
    if (box) expect(gapBetween(rubricBox, box), 'the rubric covers a card').toBeGreaterThanOrEqual(0)
  }

  await gate.getByRole('button', { name: '1 more referenced file' }).click()
  await page.getByRole('menuitem', { name: '/home/operator/private/scoring.md · judge Brief critic' }).click()
  // A file anywhere on disk opens: Archon confines no files.
  const scoring = page.getByRole('dialog', { name: 'file scoring.md' })
  await expect(scoring.getByRole('heading', { name: 'Scoring guide' })).toBeVisible()
  await expect(scoring).toContainText('/home/operator/private/scoring.md')

  // A file link in a node window opens its file near that window, clear of it.
  await page.getByTestId(`mission-node-${board.inputCards[0].id}`).locator('.mtitle').click()
  const missionWindow = page.getByRole('dialog', { name: 'Input card · Scouting' })
  await missionWindow.getByRole('button', { name: 'Open file sketch.md' }).click()
  const sketch = page.getByRole('dialog', { name: 'file sketch.md' })
  await expect(sketch.getByRole('alert')).toContainText('a relative file reference has no base: use an absolute path')
  const besideWindow = gapBetween((await sketch.boundingBox())!, (await missionWindow.boundingBox())!)
  expect(besideWindow).toBeGreaterThanOrEqual(0)
  expect(besideWindow).toBeLessThanOrEqual(240)
  expect(fixture.writes).toEqual([])
})

test('a file opened from a Flow row leaves that row\'s number, title, labels and links clickable', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  await scoutingFixture(page, { mission: boardWithFiles() })
  await page.route('**/api/files/preview?**', servePreview)
  await page.goto('/?mission=scouting')
  await page.getByRole('radio', { name: 'Flow' }).click()
  const gate = page.locator(`.flow-step[data-flow-node="${scouting.mission.gates.find((node: Node) => node.title === 'Adversarial review').id}"]`)
  await gate.scrollIntoViewIfNeeded()
  const chip = gate.getByRole('button', { name: 'adversarial-review.md' })
  await chip.click()
  await expect(page.getByRole('dialog', { name: 'file adversarial-review.md' })).toBeVisible()
  const handles = gate.locator('> .flow-body .flow-number, > .flow-body .flow-step-head .flow-title, .flow-label, .flow-link')
  expect(await handles.count()).toBeGreaterThan(3)
  for (const handle of await handles.all()) {
    const reachable = await handle.evaluate(element => {
      const rect = element.getBoundingClientRect()
      const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
      return Boolean(hit && (hit === element || element.contains(hit)))
    })
    expect(reachable, `${await handle.innerText()} stays clickable`).toBe(true)
  }
})

test('refused copying leaves a selectable path beside the file download action', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.clear()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: () => new Promise((_resolve, reject) => {
      Object.defineProperty(window, 'rejectFileCopy', { configurable: true, value: () => reject(new Error('Copy refused')) })
    }) } })
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false })
  })
  const fixture = await scoutingFixture(page, { mission: boardWithFiles() })
  const text = '# Adversarial review rubric\n\nA complete downloadable document.'
  await page.route('**/api/files/preview?**', route => route.fulfill({ json: { success: true, data: { file: {
    path: RUBRIC, name: 'adversarial-review.md', size: text.length, kind: 'markdown', text: { text, bytes: text.length },
  } } } }))
  await page.goto('/?mission=scouting')
  await page.getByRole('button', { name: `Open ${RUBRIC}`, exact: true }).click()
  const file = page.getByRole('dialog', { name: 'file adversarial-review.md' })
  await expect(file.getByRole('link', { name: 'Download', exact: true })).toHaveAttribute('download', 'adversarial-review.md')
  await file.getByRole('button', { name: 'Copy path', exact: true }).click()
  await expect(file.getByRole('button', { name: 'Copying…', exact: true })).toBeFocused()
  await page.evaluate(() => (window as unknown as { rejectFileCopy: () => void }).rejectFileCopy())
  await expect(file.getByRole('status')).toContainText('browser refused copying')
  await expect(file.getByRole('button', { name: 'Copy path', exact: true })).toBeFocused()
  const manual = file.getByRole('textbox', { name: 'Path to copy manually' })
  await expect(manual).toBeVisible()
  await manual.focus()
  expect(await manual.evaluate(input => (input as HTMLInputElement).selectionEnd! - (input as HTMLInputElement).selectionStart!)).toBe(RUBRIC.length)
  await file.getByRole('button', { name: 'Copy path', exact: true }).click()
  await expect(file.getByRole('button', { name: 'Copying…', exact: true })).toBeFocused()
  await page.evaluate(() => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => true })
    ;(window as unknown as { rejectFileCopy: () => void }).rejectFileCopy()
  })
  await expect(file.getByRole('button', { name: 'Copied', exact: true })).toBeFocused()
  await expect(file.getByRole('textbox', { name: 'Path to copy manually' })).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})
