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
  // Usage spans every mission: the judge slot here and the Scouting map.
  await expect(inspector.getByRole('region', { name: 'Used by' }).locator('.agx-role-use')).toHaveText([
    'Delivery › Beads reviewer › Beads reviewerClaude Code · default model · medium',
    'Scouting › Map › Map',
  ])
})

test('a role\'s usage is shown before Retire and Delete, and a use opens its slot', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })

  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  const used = inspector.getByRole('region', { name: 'Used by' })
  await used.getByRole('button', { name: 'Retire' }).click()
  await expect(used.getByRole('group', { name: 'Retire Brief critic' })).toContainText('The 2 slots above stop running until each has another role or none.')
  await used.getByRole('button', { name: 'Retire Brief critic' }).click()
  await expect(used.getByRole('button', { name: 'Bring back' })).toBeVisible()
  expect(fixture.patches.at(-1)).toEqual({ retire: true })
  await expect(agents.locator('.ragent', { hasText: 'Brief critic' })).toContainText('retired')
  // The slot it staffs says why it will not run.
  await expect(agents.getByTestId('agents-slot-judge-judge_seat').locator('.slot-warn')).toHaveText('role retired')
  await used.getByRole('button', { name: 'Bring back' }).click()
  await expect(used.getByRole('button', { name: 'Retire' })).toBeVisible()
  expect(fixture.patches.at(-1)).toEqual({ retire: false })

  // Delete names the slots and offers nothing that would strand them.
  await used.getByRole('button', { name: 'Delete' }).click()
  await expect(used.getByRole('group', { name: 'Delete Brief critic' })).toContainText('Brief critic staffs the 2 slots above. Give each another role or none before deleting Brief critic, or retire Brief critic instead.')
  await expect(used.getByRole('button', { name: 'Delete Brief critic' })).toHaveCount(0)
  await used.getByRole('button', { name: 'Close' }).click()

  // A use opens its slot, on its own mission.
  await used.getByRole('button', { name: 'Scouting › Map › Map' }).click()
  await expect(agents.getByRole('combobox', { name: 'Mission' })).toHaveValue('scouting')
  await expect(inspector.getByTestId('slot-staffing-words')).toContainText('Map (controller) is Brief critic on Claude Code')

  // An unused role goes after one confirmation.
  await agents.getByRole('button', { name: 'Inspect Hermes spawner' }).click()
  await expect(used.getByText('No slot uses this role.')).toBeVisible()
  await used.getByRole('button', { name: 'Delete' }).click()
  await used.getByRole('button', { name: 'Delete Hermes spawner' }).click()
  await expect(agents.getByRole('button', { name: 'Inspect Hermes spawner' })).toHaveCount(0)
  expect(fixture.deletions).toEqual(['spawner'])
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
  await expect(inspector.locator('.agx-role-use > span:first-child')).toHaveText(['Alpha scratch › Work › Work', 'Delivery › Build › Build', 'Delivery › Ship › Ship'])
  await expect(inspector.locator('.agx-role-slot-runs')).toHaveText(['', 'Codex · default model · medium', 'Codex · default model · medium'])
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

// archon-7e4t: a staffing in the Agents view is one Ctrl+Z step there.
test('a staffing or emptying in the Agents view is one Ctrl+Z step there', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const seat = agents.getByTestId('agents-slot-recheck-recheck_seat')
  await seat.click()
  await agents.getByRole('complementary', { name: 'Inspector' }).getByRole('button', { name: 'Staff Second opinion' }).click()
  await page.keyboard.type('critic')
  await page.keyboard.press('Enter')
  await expect(seat.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Brief critic | Claude Code · opus · xhigh')
  await page.mouse.click(1000, 1000)
  await page.keyboard.press('Control+z')
  await expect(seat.getByTestId('slot-caption')).toHaveAttribute('data-staffing', '')
  expect(fixture.boardPatches.at(-1)).toEqual({ assignSlot: { formationId: 'recheck', slotId: 'recheck_seat', agentId: '', harness: '', model: '', effort: '' } })

  // Emptying a slot is undone the same way, back to its own settings.
  const build = agents.getByTestId('agents-slot-build-build_seat')
  await build.click()
  await agents.getByRole('complementary', { name: 'Inspector' }).getByRole('button', { name: 'Empty Build' }).click()
  await expect(build.getByTestId('slot-caption')).toHaveAttribute('data-staffing', '')
  await page.mouse.click(1000, 1000)
  await page.keyboard.press('Control+z')
  await expect(build.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Builder | Codex · default model · medium')
  expect(fixture.boardPatches.at(-1)).toEqual({ assignSlot: { formationId: 'build', slotId: 'build_seat', agentId: 'builder', harness: 'openai-codex', model: '', effort: 'medium' } })
})

// archon-n7u.12: the Agents view follows the mission as it changes elsewhere, and acts on the current revision.
test('the Agents view follows a mission changed elsewhere and staffs on its current revision', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const build = agents.getByTestId('agents-slot-build-build_seat')
  await expect(build.getByTestId('slot-caption')).toHaveAttribute('data-staffing', /Builder/)
  await expect(agents.getByRole('button', { name: 'Refresh' })).toHaveCount(0)
  // An unassignment made outside this view, by the CLI or the canvas.
  const delivery = fixture.missions.delivery as { rev: number; etag: string; formations: Array<{ id: string; slots: Array<Record<string, unknown>> }> }
  delivery.formations[0].slots[0] = { id: 'build_seat', label: 'Build', controller: true }
  delivery.rev++
  delivery.etag = `delivery-${delivery.rev}`
  await expect(build.getByTestId('slot-caption')).toHaveAttribute('data-staffing', '')
  await expect(agents.locator('.rev')).toHaveText(`rev ${delivery.rev}`)
  // The next action starts from that revision and succeeds.
  await build.click()
  await agents.getByRole('complementary', { name: 'Inspector' }).getByRole('button', { name: 'Staff Build' }).click()
  await page.keyboard.press('Enter')
  await expect(build.getByTestId('slot-caption')).not.toHaveAttribute('data-staffing', '')
  await expect(agents.getByRole('alert')).toHaveCount(0)
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
