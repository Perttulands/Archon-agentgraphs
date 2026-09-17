import { expect, test } from '@playwright/test'
import { authoredBoard, nodeWindowsFixture } from './node-windows-fixture'

test('roster search matches names, IDs, harnesses and tags and clears without writes', async ({ page }) => {
  const fixture = await nodeWindowsFixture(page)
  const agents = [
    { id: 'scout-id', displayName: 'Evidence Finder', harnessDefault: 'openai-codex', tags: ['research'], kind: 'specialist', assignable: true },
    { id: 'critic-id', displayName: 'Brief Critic', harnessDefault: 'claude-code', tags: ['review'], kind: 'judge', assignable: true },
  ]
  await page.route('**/api/agents', route => route.fulfill({ json: { success: true, data: { agents, count: agents.length } } }))
  await page.goto('/?board=wayfinding')
  const roster = page.getByTestId('agent-roster')
  await expect(roster.locator('.ragent')).toHaveCount(2)
  const filter = roster.getByRole('searchbox', { name: 'Filter agents' })
  for (const query of [' Evidence ', 'SCOUT-ID', 'openai', 'research', 'specialist']) {
    await filter.fill(query)
    await expect(roster.locator('.ragent')).toHaveCount(1)
    await expect(roster.getByTestId('roster-agent-scout-id')).toBeVisible()
    await expect(roster.getByRole('status')).toHaveText('1 of 2 agents')
  }
  await page.screenshot({ path: test.info().outputPath('roster.png') })
  await filter.fill('no-such-persona')
  await expect(roster.getByText('No agents match this filter.')).toBeVisible()
  await expect(roster.locator('.ragent')).toHaveCount(0)
  await roster.getByRole('button', { name: 'Clear agent filter' }).click()
  await expect(filter).toHaveValue('')
  await expect(roster.locator('.ragent')).toHaveCount(2)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(filter).toBeVisible()
  await filter.fill('research')
  await expect(roster.getByTestId('roster-agent-scout-id')).toBeVisible()
  await expect(roster.getByTestId('roster-agent-scout-id')).toBeInViewport({ ratio: 1 })
  await page.screenshot({ path: test.info().outputPath('roster-narrow.png') })
  expect(fixture.writes).toEqual([])
})

test('brief shortcuts and a confirmed save receipt accompany the note purpose helper', async ({ page }) => {
  const fixture = await nodeWindowsFixture(page)
  let board = structuredClone(authoredBoard)
  let saves = 0
  await page.route('**/api/formations/boards/wayfinding', async route => {
    if (route.request().method() === 'PATCH') {
      const patch = route.request().postDataJSON().setBrief
      board = { ...board, rev: board.rev + 1, formations: board.formations.map((formation: { id: string }) => formation.id === patch.formationId ? { ...formation, brief: { goal: patch.goal } } : formation) }
      saves++
    }
    await route.fulfill({ json: { success: true, data: { board } }, headers: { ETag: 'edited-fixture' } })
  })
  await page.goto('/?board=wayfinding')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  await page.getByRole('button', { name: '1 Map the territory', exact: true }).click()
  const win = page.getByRole('dialog', { name: 'Formation · Map the territory' })
  await win.getByRole('button', { name: 'Edit brief' }).click()
  await expect(win.getByText('Ctrl/Cmd+Enter to save · Esc to cancel')).toBeVisible()
  const brief = win.getByRole('textbox', { name: 'Brief', exact: true })
  await brief.fill('Updated fixture brief')
  await brief.press('Control+Enter')
  await expect(win.getByRole('status')).toHaveText('Brief saved.')
  await expect(win.getByText('Updated fixture brief')).toBeVisible()
  expect(saves).toBe(1)
  await page.screenshot({ path: test.info().outputPath('saved.png') })
  await win.getByRole('button', { name: 'Edit brief' }).click()
  await expect(win.getByRole('status')).toHaveCount(0)
  await brief.press('Escape')
  await expect(win).toBeVisible()
  await expect(brief).toHaveCount(0)
  await page.getByRole('button', { name: 'Board notes', exact: true }).click()
  const notes = page.getByRole('dialog', { name: 'board notes' })
  await expect(notes.getByText('Notes record intent. Agents must incorporate them into briefs to change the work.')).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('notes.png') })
  expect(fixture.writes).toEqual([])
})
