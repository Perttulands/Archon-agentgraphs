import { expect, test } from '@playwright/test'
import { agentsFixture } from './agents-fixture'

test.use({ viewport: { width: 1920, height: 1080 } })

test('the current mission survives a visit to Agents, a reload and a fresh open', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=scouting')
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'scouting')
  // No tab, label, button or accessible name calls the unit a board.
  const boardWords = () => page.evaluate(() => [document.body.innerText, ...[...document.querySelectorAll('[aria-label],[title],[placeholder]')]
    .flatMap(element => ['aria-label', 'title', 'placeholder'].map(name => element.getAttribute(name) || ''))].join('\n').match(/\bboards?\b/gi) || [])
  expect(await boardWords()).toEqual([])

  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await expect(agents.getByRole('button', { name: 'Inspect Builder' })).toBeVisible()
  expect(await boardWords()).toEqual([])
  await page.getByRole('button', { name: 'Missions', exact: true }).click()
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'scouting')

  await page.reload()
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'scouting')

  // A fresh open with no query lands on the last used mission, not the first by name.
  await page.goto('/')
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'scouting')
})

// archon-4yos: the Agents view is the library of roles and characters; it draws no mission.
test('the Agents view opens on a role and lists its slots in every mission, with no mission drawn', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const detail = agents.getByRole('main', { name: 'Role' })

  await expect(agents.getByRole('complementary', { name: 'Agent roster' }).locator('.roster-hd .s')).toHaveText('3 roles')
  // The first role is shown without a click.
  await expect(agents.getByRole('button', { name: 'Inspect Brief critic' })).toHaveAttribute('aria-pressed', 'true')
  await expect(detail.locator('.agx-identity .n')).toHaveText('in 2 slots')
  await expect(detail.getByRole('region', { name: 'Used by' }).locator('.agx-role-use')).toHaveText([
    'Delivery › Beads reviewer › Beads reviewer',
    'Scouting › Map › Map',
  ])
  await expect(agents.getByRole('combobox')).toHaveCount(0)
  await expect(agents.locator('.missioncard, section.formation, .gatecard')).toHaveCount(0)
})

test('a role\'s usage is shown before Retire and Delete, and a use opens its mission', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const inspector = agents.getByRole('main', { name: 'Role' })

  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  const used = inspector.getByRole('region', { name: 'Used by' })
  await used.getByRole('button', { name: 'Retire' }).click()
  await expect(used.getByRole('group', { name: 'Retire Brief critic' })).toContainText('The 2 slots above stop running until each has another role or none.')
  await used.getByRole('button', { name: 'Retire Brief critic' }).click()
  await expect(used.getByRole('button', { name: 'Bring back' })).toBeVisible()
  expect(fixture.patches.at(-1)).toEqual({ retire: true })
  await expect(agents.locator('.ragent', { hasText: 'Brief critic' })).toContainText('retired')
  await used.getByRole('button', { name: 'Bring back' }).click()
  await expect(used.getByRole('button', { name: 'Retire' })).toBeVisible()
  expect(fixture.patches.at(-1)).toEqual({ retire: false })

  // Delete names the slots and offers nothing that would strand them.
  await used.getByRole('button', { name: 'Delete' }).click()
  await expect(used.getByRole('group', { name: 'Delete Brief critic' })).toContainText('Brief critic staffs the 2 slots above. Give each another role or none before deleting Brief critic, or retire Brief critic instead.')
  await expect(used.getByRole('button', { name: 'Delete Brief critic' })).toHaveCount(0)
  await used.getByRole('button', { name: 'Close' }).click()

  // An unused role goes after one confirmation.
  await agents.getByRole('button', { name: 'Inspect Hermes spawner' }).click()
  await expect(used.getByText('No slot uses this role.')).toBeVisible()
  await used.getByRole('button', { name: 'Delete' }).click()
  await used.getByRole('button', { name: 'Delete Hermes spawner' }).click()
  await expect(agents.getByRole('button', { name: 'Inspect Hermes spawner' })).toHaveCount(0)
  expect(fixture.deletions).toEqual(['spawner'])

  // A use opens its mission on Missions.
  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  await used.getByRole('button', { name: 'Scouting › Map › Map' }).click()
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'scouting')
  await expect(page).toHaveURL(/\?mission=scouting$/)
})

test('a role is role text: its page and editor offer no model or effort', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')

  await agents.getByRole('button', { name: 'Inspect Builder' }).click()
  const inspector = agents.getByRole('main', { name: 'Role' })
  await expect(inspector.locator('.agx-identity .n')).toHaveText('in 3 slots')
  // The settings live on the slots the role staffs, never on the role.
  await expect(inspector.locator('.agx-role-use > span:first-child')).toHaveText(['Alpha scratch › Work › Work', 'Delivery › Build › Build', 'Delivery › Ship › Ship'])
  await expect(inspector.getByText(/harness variants|\bRuns\b|effort medium|starts as/i)).toHaveCount(0)
  await expect(inspector.getByRole('textbox', { name: /model/i })).toHaveCount(0)
  await expect(inspector.getByRole('combobox', { name: /effort/i })).toHaveCount(0)

  await inspector.getByRole('button', { name: 'Edit role' }).click()
  const editor = page.getByTestId('persona-editor')
  await expect(editor.getByLabel('Role display name')).toHaveValue('Builder')
  await expect(editor.getByText(/model|effort|harness variant/i)).toHaveCount(0)
  await editor.getByLabel('Role summary').fill('Builds the change, test first.')
  await editor.getByRole('button', { name: 'Save role' }).click()
  await expect(editor).toHaveCount(0)
  expect(fixture.patches.at(-1)).toEqual({ displayName: 'Builder', kind: 'builder', summary: 'Builds the change, test first.', capabilities: ['implement'] })

  // A new role asks only for role text.
  await agents.getByRole('button', { name: 'New role' }).click()
  const create = page.getByRole('dialog', { name: 'New role' })
  await expect(create.getByText(/model|effort/i)).toHaveCount(0)
  await expect(create.getByLabel('Harness')).toHaveCount(0)
})

// archon-n7u.14: a role's notes are listed in the inspector, and a note written here is the operator's.
test('the role inspector lists its notes and a note written here is the operator\'s', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await agents.locator('.ragent', { hasText: 'Brief critic' }).click()
  const notes = agents.getByRole('list', { name: 'Notes on Brief critic' })
  await expect(notes.locator('.note-entry')).toHaveCount(1)
  await expect(notes.locator('.note-entry').first()).toContainText('archon')
  await expect(notes.locator('.note-entry').first()).toContainText('Seeded from the catalog cleanup.')
  await agents.getByLabel('Add note').fill('Prefers short verdicts.')
  await agents.getByRole('button', { name: 'Save note' }).click()
  await expect(notes.locator('.note-entry')).toHaveCount(2)
  await expect(notes.locator('.note-entry').last().locator('.note-author')).toHaveText('operator')
  await expect(notes.locator('.note-entry').last()).toContainText('Prefers short verdicts.')
  await expect(agents.getByLabel('Add note')).toHaveValue('')
  expect(fixture.patches.at(-1)).toEqual({ note: 'Prefers short verdicts.', updatedBy: 'human:ui' })
})
