import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Node = { id: string; title: string; type?: string; files?: string[]; brief?: { goal?: string; files?: string[] } }
type Box = { x: number; y: number; width: number; height: number }

// How far a window sits from a box: 0 or more when clear of it, negative when it covers it.
const gapBetween = (win: Box, box: Box) =>
  Math.max(win.x - (box.x + box.width), box.x - (win.x + win.width), win.y - (box.y + box.height), box.y - (win.y + win.height))

// Wayfinding with the reference files an operator would attach: a rubric on
// the adversarial review gate, a scoring guide in its judge's brief, and a
// sketch on the mission.
function boardWithFiles() {
  const board = structuredClone(wayfinding.board)
  const byTitle = (nodes: Node[], title: string) => nodes.find(node => node.title === title)!
  byTitle(board.missions, 'Wayfinding').files = ['/srv/projects/wayfinding/sketch.md']
  byTitle(board.gates, 'Adversarial review').files = ['rubrics/adversarial-review.md']
  const critic = byTitle(board.formations, 'Brief critic')
  critic.brief = { ...critic.brief, files: ['/home/operator/private/scoring.md'] }
  return board
}

// The room Arrange reserves for each card (arrangementItemSize in
// src/internal/formations/layout_arrange.go), with its file chip row.
const FILE_ROW = 30
const reserved = (node: Node, kind: 'mission' | 'gate' | 'formation') =>
  (kind === 'mission' ? 144 : kind === 'gate' ? 124 : node.type === 'peer' ? 340 : node.type === 'orchestrated' ? 440 : 310) + FILE_ROW

test('a gate\'s rubric and its judge\'s brief file open from the gate on Wayfinding', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const board = boardWithFiles()
  const fixture = await wayfindingFixture(page, { board })
  await page.route('**/api/formations/files/preview?**', route => {
    const path = new URL(route.request().url()).searchParams.get('path') || ''
    if (path !== 'rubrics/adversarial-review.md') {
      return route.fulfill({ status: 403, json: { success: false, error: { code: 'Forbidden', message: "file is not readable here: it is outside the daemon's file roots" } } })
    }
    const text = '# Adversarial review rubric\n\nFail a brief whose recommendation would fit any project.'
    return route.fulfill({ json: { success: true, data: { file: { path, name: 'adversarial-review.md', size: text.length, modifiedAt: '', kind: 'markdown', text: { text, bytes: text.length } } } } })
  })
  await page.goto('/?board=wayfinding')
  await expect(page.getByRole('note')).toHaveCount(wayfinding.notes.elements.length)

  const cards: Array<[Node, 'mission' | 'gate' | 'formation']> = [
    [board.missions[0], 'mission'],
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
  await gate.getByRole('button', { name: 'Open rubrics/adversarial-review.md' }).click()
  const rubric = page.getByRole('dialog', { name: 'file adversarial-review.md' })
  await expect(rubric.getByRole('heading', { name: 'Adversarial review rubric' })).toBeVisible()
  await expect(rubric).toContainText('Adversarial review · rubrics/adversarial-review.md')
  // A chip opens its file beside the card, not the card's node window.
  await expect(page.getByRole('dialog', { name: 'Gate · Adversarial review' })).toHaveCount(0)
  const besideGate = gapBetween((await rubric.boundingBox())!, (await gate.boundingBox())!)
  expect(besideGate).toBeGreaterThanOrEqual(0)
  expect(besideGate).toBeLessThanOrEqual(16)

  await gate.getByRole('button', { name: '1 more referenced file' }).click()
  await page.getByRole('menuitem', { name: '/home/operator/private/scoring.md · judge Brief critic' }).click()
  const outside = page.getByRole('dialog', { name: 'file scoring.md' })
  await expect(outside.getByRole('alert')).toContainText('file is not readable here')
  await expect(outside).toContainText('/home/operator/private/scoring.md')

  // A file link in a node window opens its file beside that window.
  await page.getByTestId(`mission-node-${board.missions[0].id}`).locator('.mtitle').click()
  const missionWindow = page.getByRole('dialog', { name: 'Mission · Wayfinding' })
  await missionWindow.getByRole('button', { name: 'Open file /srv/projects/wayfinding/sketch.md' }).click()
  const sketch = page.getByRole('dialog', { name: 'file sketch.md' })
  await expect(sketch.getByRole('alert')).toContainText('file is not readable here')
  const besideWindow = gapBetween((await sketch.boundingBox())!, (await missionWindow.boundingBox())!)
  expect(besideWindow).toBeGreaterThanOrEqual(0)
  expect(besideWindow).toBeLessThanOrEqual(16)
  expect(fixture.writes).toEqual([])
})
