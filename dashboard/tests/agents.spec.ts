import { expect, test } from '@playwright/test'
import { agentsFixture } from './agents-fixture'

test.use({ viewport: { width: 1920, height: 1080 } })

test('Missions and Agents share one current mission across view switches, reloads and fresh opens', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=scouting')
  await expect(page.getByTestId('board-picker')).toHaveValue('scouting')
  // No tab, label, button or accessible name calls the unit a board.
  const boardWords = () => page.evaluate(() => [document.body.innerText, ...[...document.querySelectorAll('[aria-label],[title],[placeholder]')]
    .flatMap(element => ['aria-label', 'title', 'placeholder'].map(name => element.getAttribute(name) || ''))].join('\n').match(/\bboards?\b/gi) || [])
  expect(await boardWords()).toEqual([])

  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await expect(agents.getByRole('combobox', { name: 'Mission' })).toHaveValue('scouting')
  expect(await boardWords()).toEqual([])
  await expect(agents.locator('section.formation .tt')).toHaveText(['Map'])

  await agents.getByRole('combobox', { name: 'Mission' }).selectOption('delivery')
  await expect(page).toHaveURL(/\?mission=delivery$/)
  await page.getByRole('button', { name: 'Missions', exact: true }).click()
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')

  await page.reload()
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')

  // A fresh open with no query lands on the last used board, not the first by name.
  await page.goto('/')
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await expect(agents.getByRole('combobox', { name: 'Mission' })).toHaveValue('delivery')
})

test('a mission is not ready while a slot in its judge chain is open', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')

  await expect(agents.locator('.missioncard')).toContainText('3/4 slots staffed · 1 open')
  await expect(agents.locator('.missioncard')).not.toContainText('ready')
  await expect(agents.locator('.gatecard')).toContainText('judged by Beads reviewer → Second opinion')
  await expect(agents.locator('section.formation', { hasText: 'Second opinion' })).toContainText('judges Beads review')
  await expect(agents.getByRole('complementary', { name: 'Agent roster' }).locator('.roster-hd .s')).toHaveText('3 roles · 2 in use')

  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByText('Slots on this mission')).toBeVisible()
  await expect(inspector.locator('.tool-detail-row', { hasText: 'Beads reviewer' })).toBeVisible()
  await expect(inspector.getByText('No slots on this mission.')).toHaveCount(0)
})

test('a role is role text: the inspector and editor offer no model or effort, and each slot says what its seat runs', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')

  await agents.getByRole('button', { name: 'Inspect Builder' }).click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.locator('.agx-identity .n')).toHaveText('in 2 slots')
  // The settings live on the slots the role staffs, never on the role.
  await expect(inspector.locator('.agx-role-slot > span:first-child')).toHaveText(['Build', 'Ship'])
  await expect(inspector.locator('.agx-role-slot-runs')).toHaveText(['Codex · default model · medium', 'Codex · default model · medium'])
  await expect(inspector.getByText(/harness variants|\bRuns\b|effort medium|starts as/i)).toHaveCount(0)
  await expect(inspector.getByRole('textbox', { name: /model/i })).toHaveCount(0)
  await expect(inspector.getByRole('combobox', { name: /effort/i })).toHaveCount(0)

  await inspector.getByRole('button', { name: 'Edit persona' }).click()
  const editor = page.getByTestId('persona-editor')
  await expect(editor.getByLabel('Agent display name')).toHaveValue('Builder')
  await expect(editor.getByText(/model|effort|harness variant/i)).toHaveCount(0)
  await editor.getByLabel('Agent summary').fill('Builds the change, test first.')
  await editor.getByRole('button', { name: 'Save agent override' }).click()
  await expect(editor).toHaveCount(0)
  expect(fixture.patches.at(-1)).toEqual({ displayName: 'Builder', kind: 'builder', summary: 'Builds the change, test first.', capabilities: ['implement'], sessionStem: 'builder' })

  // A new role asks only for role text.
  await agents.getByRole('button', { name: 'New agent' }).click()
  const create = page.getByRole('dialog', { name: 'Create persona' })
  await expect(create.getByText(/model|effort/i)).toHaveCount(0)
  await expect(create.getByLabel('Harness')).toHaveCount(0)
})
